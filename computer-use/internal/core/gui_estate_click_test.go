package core

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/Hybrid-Computer-Use/computer-use/internal/contract"
)

const testHandleWebull = "w2:dddddddddddddddddddddddddddddddd"

func webullDellApps() []AppDescriptor {
	return []AppDescriptor{{
		Name: "Webull Desktop", BundleIdentifier: "com.webull.desktop", PID: 99, InputCapable: true,
		ProviderBusName: ":1.99", ProcessGeneration: "pid:99:start:1",
		Windows: []ProviderAppWindow{{
			Title: "", Role: "window", Handle: testHandleWebull,
			ProviderBusName: ":1.99", ProviderObjectPath: "/w/webull",
			ProcessGeneration: "pid:99:start:1", Active: true, InputTarget: true,
			Bounds: &frame{X: 0, Y: -2160, Width: 3840, Height: 2160},
		}},
	}}
}

func estateDesktopSnapshot(t *testing.T) *AppSnapshot {
	t.Helper()
	return &AppSnapshot{
		ScreenshotPNGBase64: testPNGBase64(t, 780, 768),
		ScreenshotMimeType:  "image/png",
		CoordinateSpace:     "screen_image_px",
		FrameID:             "estate-frame",
		Width:               780,
		Height:              768,
		DesktopLayout: map[string]any{
			"desktop": map[string]any{
				"x": 0.0, "y": -2160.0, "width": 5120.0, "height": 5040.0,
			},
		},
	}
}

func TestEstateWindowClickMapsFractionsIntoNativeFrame(t *testing.T) {
	// Dell 32 Webull as estate RPA records it: origin top-left, PROFILE
	// fractions of that window. A mapping that used screenshot pixels or
	// a primary-display origin would miss the window.
	dell := frame{X: 0, Y: -2160, Width: 3840, Height: 2160}

	x, y, err := estateNativeClick(dell, 0.08, 0.04)
	if err != nil {
		t.Fatalf("search_box mapping failed: %v", err)
	}
	if x != 307 || y != -2074 {
		t.Fatalf("search_box native click = (%v, %v), want (307, -2074)", x, y)
	}

	x, y, err = estateNativeClick(dell, 0.5, 0.5)
	if err != nil {
		t.Fatalf("window center mapping failed: %v", err)
	}
	if x != 1920 || y != -1080 {
		t.Fatalf("window center native click = (%v, %v), want (1920, -1080)", x, y)
	}

	calc := frame{X: 100, Y: 200, Width: 400, Height: 500}
	x, y, err = estateNativeClick(calc, 0.5, 0.5)
	if err != nil {
		t.Fatalf("Calculator-style mapping failed: %v", err)
	}
	if x != 300 || y != 450 {
		t.Fatalf("Calculator center native click = (%v, %v), want (300, 450)", x, y)
	}
}

func TestEstateNativePointMapsIntoGUIScreenshotPixels(t *testing.T) {
	// Studio + Dell union as HCU composites it: native origin at the top-left
	// of the Dell (y=-2160). Delivered size is the 1366×768-bounded screenshot.
	desktop := frame{X: 0, Y: -2160, Width: 5120, Height: 5040}
	const deliveredW, deliveredH = 780, 768

	x, y, err := screenshotPointFromNative(desktop, deliveredW, deliveredH, 307, -2074)
	if err != nil {
		t.Fatalf("search_box screenshot mapping failed: %v", err)
	}
	// Inverse of the provider's pixel-center map:
	// delivered = (native - origin + 0.5) * delivered / native - 0.5
	if math.Abs(x-46.345703125) > 1e-9 || math.Abs(y-12.68095238095238) > 1e-9 {
		t.Fatalf("search_box screenshot click = (%v, %v), want (46.345703125, 12.68095238095238)", x, y)
	}

	cx, cy, err := screenshotPointFromNative(desktop, deliveredW, deliveredH, 1920, -1080)
	if err != nil {
		t.Fatalf("window center screenshot mapping failed: %v", err)
	}
	if math.Abs(cx-292.076171875) > 1e-9 || math.Abs(cy-164.14761904761905) > 1e-9 {
		t.Fatalf("window center screenshot click = (%v, %v), want (292.076171875, 164.14761904761905)", cx, cy)
	}
}

func TestEstateWindowClickRejectsFractionsOutsideTheWindow(t *testing.T) {
	window := frame{X: 0, Y: 0, Width: 100, Height: 100}
	for _, tc := range []struct {
		fx, fy float64
	}{
		{-0.01, 0.5},
		{1.01, 0.5},
		{0.5, -0.01},
		{0.5, 1.01},
		{math.NaN(), 0.5},
		{0.5, math.Inf(1)},
	} {
		if _, _, err := estateNativeClick(window, tc.fx, tc.fy); err == nil {
			t.Fatalf("accepted fraction (%v, %v) outside the window", tc.fx, tc.fy)
		}
	}
}

func TestAppsListsEstateWindowBounds(t *testing.T) {
	service, _ := windowScriptService(webullDellApps())
	text := ResultText(service.listApps())
	if !strings.Contains(text, `w1 ""  app=Webull Desktop  active  bounds=[0,-2160 3840x2160]`) {
		t.Fatalf("apps listing omitted estate bounds:\n%s", text)
	}
}

func TestGUIClickWithWindowRefUsesEstateWindowFrame(t *testing.T) {
	service, backend := windowScriptService(webullDellApps())
	apps := ResultText(service.listApps())
	if !strings.Contains(apps, `w1 ""  app=Webull Desktop  active`) {
		t.Fatalf("apps did not issue w1 for the untitled Webull window:\n%s", apps)
	}
	service.guiSnapshot = estateDesktopSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 780, 768)

	result := service.callTool(contract.PublicToolGUI, map[string]any{
		"actions": []any{map[string]any{
			"type": "click", "window_ref": "w1", "fx": 0.08, "fy": 0.04,
		}},
	})
	if result.IsError {
		t.Fatalf("estate window click was rejected: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 1 {
		t.Fatalf("want one click batch, got %#v", backend.desktopRequests)
	}
	click := backend.desktopRequests[0]
	if click.FrameID != "" || len(click.Actions) != 1 || click.Actions[0].Type != "click" {
		t.Fatalf("estate click was not dispatched as a GUI click without screenshot frame_id: %#v", click)
	}
	action := click.Actions[0]
	if action.X == nil || action.Y == nil {
		t.Fatalf("estate click did not become screenshot pixels: %#v", action)
	}
	if math.Abs(*action.X-46.345703125) > 1e-9 || math.Abs(*action.Y-12.68095238095238) > 1e-9 {
		t.Fatalf("estate click screenshot pixels = (%v, %v), want (46.345703125, 12.68095238095238)", *action.X, *action.Y)
	}
}

func TestGUIClickByTitleUsesEstateWindowFrameWhenWindowTitleEmpty(t *testing.T) {
	service, backend := windowScriptService(webullDellApps())
	service.guiSnapshot = estateDesktopSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 780, 768)

	result := service.callTool(contract.PublicToolGUI, map[string]any{
		"actions": []any{map[string]any{
			"type": "click", "title": "Webull", "fx": 0.5, "fy": 0.5,
		}},
	})
	if result.IsError {
		t.Fatalf("title-directed estate click was rejected: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 1 || len(backend.desktopRequests[0].Actions) != 1 {
		t.Fatalf("want one click batch, got %#v", backend.desktopRequests)
	}
	action := backend.desktopRequests[0].Actions[0]
	if action.X == nil || action.Y == nil {
		t.Fatalf("title-directed click did not become screenshot pixels: %#v", action)
	}
	if math.Abs(*action.X-292.076171875) > 1e-9 || math.Abs(*action.Y-164.14761904761905) > 1e-9 {
		t.Fatalf("title-directed screenshot pixels = (%v, %v), want (292.076171875, 164.14761904761905)", *action.X, *action.Y)
	}
}

func TestGUIClickHonorsEstateRPAPause(t *testing.T) {
	pause := filepath.Join(t.TempDir(), "rpa-pause.json")
	if err := os.WriteFile(pause, []byte(`{"schema":"ggaix.rpa-pause.v1","paused":true,"reason":"UNIT_TEST_HOLD"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GGAIX_RPA_PAUSE_PATH", pause)

	service, backend := windowScriptService(webullDellApps())
	service.guiSnapshot = estateDesktopSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 780, 768)

	result := service.callTool(contract.PublicToolGUI, map[string]any{
		"actions": []any{map[string]any{
			"type": "click", "window_ref": "w1", "fx": 0.08, "fy": 0.04,
		}},
	})
	if !result.IsError || !strings.Contains(ResultText(result), "RPA_PAUSED:UNIT_TEST_HOLD") {
		t.Fatalf("paused estate click must fail closed: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 0 {
		t.Fatalf("paused click reached the provider: %#v", backend.desktopRequests)
	}
}

func TestGUIClickProceedsWhenEstateRPAPauseIsInactive(t *testing.T) {
	pause := filepath.Join(t.TempDir(), "rpa-pause.json")
	if err := os.WriteFile(pause, []byte(`{"schema":"ggaix.rpa-pause.v1","paused":false,"reason":"uat_resume_active"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GGAIX_RPA_PAUSE_PATH", pause)

	service, backend := windowScriptService(webullDellApps())
	_ = service.listApps()
	service.guiSnapshot = estateDesktopSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 780, 768)

	result := service.callTool(contract.PublicToolGUI, map[string]any{
		"actions": []any{map[string]any{
			"type": "click", "window_ref": "w1", "fx": 0.08, "fy": 0.04,
		}},
	})
	if result.IsError {
		t.Fatalf("active pause file blocked a click: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 1 {
		t.Fatalf("want the click dispatched, got %#v", backend.desktopRequests)
	}
}

func TestAXClickHonorsEstateRPAPause(t *testing.T) {
	pause := filepath.Join(t.TempDir(), "rpa-pause.json")
	if err := os.WriteFile(pause, []byte(`{"schema":"ggaix.rpa-pause.v1","paused":true,"reason":"UNIT_TEST_HOLD"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GGAIX_RPA_PAUSE_PATH", pause)

	service, _, _, elementRef := sheetBatchService(t)
	backend := &recordingPlatformBackend{}
	service.Backend = backend
	result := service.callTool(contract.PublicToolAX, map[string]any{
		"mode": "act",
		"actions": []any{map[string]any{
			"type": "click", "ref": elementRef,
		}},
	})
	if !result.IsError || !strings.Contains(ResultText(result), "RPA_PAUSED:UNIT_TEST_HOLD") {
		t.Fatalf("paused AX click must fail closed: %s", ResultText(result))
	}
	if len(backend.actionRequests) != 0 || len(backend.calls) != 0 {
		t.Fatalf("paused AX click reached the provider: calls=%v actions=%v", backend.calls, backend.actionRequests)
	}
}

func TestGUIClickAcceptsJSONNumberFractionsFromDaemonWire(t *testing.T) {
	service, backend := windowScriptService(webullDellApps())
	_ = service.listApps()
	service.guiSnapshot = estateDesktopSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 780, 768)

	decoder := json.NewDecoder(bytes.NewReader([]byte(
		`{"actions":[{"type":"click","window_ref":"w1","fx":0.08,"fy":0.04}]}`,
	)))
	decoder.UseNumber()
	var args map[string]any
	if err := decoder.Decode(&args); err != nil {
		t.Fatal(err)
	}
	result := service.callTool(contract.PublicToolGUI, args)
	if result.IsError {
		t.Fatalf("daemon-wire estate click was rejected: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 1 || len(backend.desktopRequests[0].Actions) != 1 {
		t.Fatalf("want one click batch, got %#v", backend.desktopRequests)
	}
	action := backend.desktopRequests[0].Actions[0]
	if action.X == nil || action.Y == nil {
		t.Fatalf("json.Number estate click did not become screenshot pixels: %#v", action)
	}
	if math.Abs(*action.X-46.345703125) > 1e-9 || math.Abs(*action.Y-12.68095238095238) > 1e-9 {
		t.Fatalf("json.Number estate click screenshot pixels = (%v, %v)", *action.X, *action.Y)
	}
}

func TestGUIPixelClickKeepsScreenshotCoordinatesWhenWindowRefPresent(t *testing.T) {
	service, backend := windowScriptService(webullDellApps())
	_ = service.listApps()
	service.guiSnapshot = estateDesktopSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 780, 768)

	result := service.callTool(contract.PublicToolGUI, map[string]any{
		"actions": []any{map[string]any{
			"type": "click", "x": 10.0, "y": 20.0, "window_ref": "w1",
		}},
	})
	if result.IsError {
		t.Fatalf("screenshot pixel click was rewritten as an estate click: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 1 || len(backend.desktopRequests[0].Actions) != 1 {
		t.Fatalf("want one pixel click, got %#v", backend.desktopRequests)
	}
	action := backend.desktopRequests[0].Actions[0]
	if action.X == nil || action.Y == nil || *action.X != 10 || *action.Y != 20 {
		t.Fatalf("pixel click coordinates changed: %#v", action)
	}
	if backend.desktopRequests[0].FrameID != "estate-frame" {
		t.Fatalf("pixel click dropped screenshot frame_id: %#v", backend.desktopRequests[0])
	}
}

const testHandleCalc = "w2:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

func calculatorStudioApps() []AppDescriptor {
	return []AppDescriptor{{
		Name: "Calculator", BundleIdentifier: "com.apple.calculator", PID: 50, InputCapable: true,
		ProviderBusName: ":1.50", ProcessGeneration: "pid:50:start:1",
		Windows: []ProviderAppWindow{{
			Title: "Calculator", Role: "window", Handle: testHandleCalc,
			ProviderBusName: ":1.50", ProviderObjectPath: "/w/calc",
			ProcessGeneration: "pid:50:start:1", Active: true, InputTarget: true,
			Bounds: &frame{X: -1730, Y: 681, Width: 230, Height: 408},
		}},
	}}
}

func liveNativePNGSnapshot(t *testing.T) *AppSnapshot {
	t.Helper()
	// Tiny PNG: rewrite maps from Width/Height and desktop layout, not pixels.
	return &AppSnapshot{
		ScreenshotPNGBase64: testPNGBase64(t, 8, 8),
		ScreenshotMimeType:  "image/png",
		CoordinateSpace:     "screen_image_px",
		FrameID:             "live-native-png",
		Width:               6400,
		Height:              3600,
		DesktopLayout: map[string]any{
			"desktop": map[string]any{
				"x": -2560.0, "y": -2160.0, "width": 6400.0, "height": 3600.0,
			},
		},
	}
}

func TestGUIEstateClickMapsIntoProviderDeliveredBoundsWhenScreenshotPNGIsNativeSize(t *testing.T) {
	// Live Studio+Dell capture stays 6400×3600 because the shrink scale (~0.213)
	// is below screenshotResultMinScale 0.25. executeGUI still validates and
	// maps clicks against desktopDeliveredBounds 1365×768. Estate rewrite must
	// emit that delivered space, not native-relative PNG pixels (945, 3045).
	service, backend := windowScriptService(calculatorStudioApps())
	_ = service.listApps()
	service.guiSnapshot = liveNativePNGSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 1365, 768)

	result := service.callTool(contract.PublicToolGUI, map[string]any{
		"actions": []any{map[string]any{
			"type": "click", "title": "Calculator", "fx": 0.5, "fy": 0.5,
		}},
	})
	if result.IsError {
		t.Fatalf("live-shaped estate click was rejected: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 1 || len(backend.desktopRequests[0].Actions) != 1 {
		t.Fatalf("want one click batch, got %#v", backend.desktopRequests)
	}
	action := backend.desktopRequests[0].Actions[0]
	if action.X == nil || action.Y == nil {
		t.Fatalf("estate click did not become screenshot pixels: %#v", action)
	}
	if *action.X == 945 && *action.Y == 3045 {
		t.Fatalf("estate click used native PNG pixels (945, 3045); provider validates 1365x768")
	}
	wantX, wantY, err := screenshotPointFromNative(
		frame{X: -2560, Y: -2160, Width: 6400, Height: 3600},
		1365, 768,
		-1615, 885,
	)
	if err != nil {
		t.Fatalf("delivered mapping: %v", err)
	}
	if math.Abs(*action.X-wantX) > 1e-9 || math.Abs(*action.Y-wantY) > 1e-9 {
		t.Fatalf("estate click screenshot pixels = (%v, %v), want delivered (%v, %v)", *action.X, *action.Y, wantX, wantY)
	}
}

func TestGUIEstateClickOmitsScreenshotFrameID(t *testing.T) {
	// Estate fx/fy is live AX window geometry, not a pixel of the last PNG.
	// Sending that PNG's frame_id makes a busy desktop (unrelated windows
	// moving during the AX listing) reject the click as gui_frame_stale.
	service, backend := windowScriptService(calculatorStudioApps())
	_ = service.listApps()
	service.guiSnapshot = liveNativePNGSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 1365, 768)

	result := service.callTool(contract.PublicToolGUI, map[string]any{
		"actions": []any{map[string]any{
			"type": "click", "title": "Calculator", "fx": 0.5, "fy": 0.5,
		}},
	})
	if result.IsError {
		t.Fatalf("estate click was rejected: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 1 {
		t.Fatalf("want one click batch, got %#v", backend.desktopRequests)
	}
	if backend.desktopRequests[0].FrameID != "" {
		t.Fatalf("estate click sent screenshot frame_id %q; window-list motion would stale it", backend.desktopRequests[0].FrameID)
	}
}

func TestGUIClickFailClosedOnInvalidEstateRPAPauseFile(t *testing.T) {
	pause := filepath.Join(t.TempDir(), "rpa-pause.json")
	if err := os.WriteFile(pause, []byte(`{"paused":"yes"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GGAIX_RPA_PAUSE_PATH", pause)

	service, backend := windowScriptService(webullDellApps())
	_ = service.listApps()
	service.guiSnapshot = estateDesktopSnapshot(t)
	backend.desktopResponse = testDesktopFrame(t, "after-click", 780, 768)

	result := service.callTool(contract.PublicToolGUI, map[string]any{
		"actions": []any{map[string]any{"type": "click", "x": 10.0, "y": 20.0}},
	})
	if !result.IsError || !strings.Contains(ResultText(result), "RPA_PAUSED:") {
		t.Fatalf("invalid pause file must fail closed: %s", ResultText(result))
	}
	if len(backend.desktopRequests) != 0 {
		t.Fatalf("invalid pause file still clicked: %#v", backend.desktopRequests)
	}
}
