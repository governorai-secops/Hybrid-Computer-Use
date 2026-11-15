package core

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// estateNativeClick maps a fraction of an estate window frame (origin at the
// window's top-left, the same PROFILE coordinates paper RPA uses) to a native
// desktop point. fx and fy are 0..1 inclusive.
func estateNativeClick(bounds frame, fx, fy float64) (float64, float64, error) {
	if err := estateFraction(fx, "fx"); err != nil {
		return 0, 0, err
	}
	if err := estateFraction(fy, "fy"); err != nil {
		return 0, 0, err
	}
	if bounds.Width <= 0 || bounds.Height <= 0 ||
		math.IsNaN(bounds.Width) || math.IsNaN(bounds.Height) ||
		math.IsInf(bounds.Width, 0) || math.IsInf(bounds.Height, 0) {
		return 0, 0, fmt.Errorf("window frame must have positive width and height")
	}
	x := math.Round(bounds.X + fx*bounds.Width)
	y := math.Round(bounds.Y + fy*bounds.Height)
	return x, y, nil
}

func estateFraction(value float64, name string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return fmt.Errorf("%s must be a finite fraction in [0, 1]", name)
	}
	return nil
}

// GUI screenshots and executeGUI share this cap: the provider validates
// clicks against desktopDeliveredBounds, not the PNG's encoded pixels.
const (
	guiScreenshotMaxWidth  = 1366
	guiScreenshotMaxHeight = 768
)

func guiDeliveredSize(desktop frame) (int, int) {
	scale := math.Min(1, math.Min(
		float64(guiScreenshotMaxWidth)/desktop.Width,
		float64(guiScreenshotMaxHeight)/desktop.Height,
	))
	width := int(math.Max(1, math.Round(desktop.Width*scale)))
	height := int(math.Max(1, math.Round(desktop.Height*scale)))
	return width, height
}

// screenshotPointFromNative is the inverse of the macOS/Linux provider
// pixel-center map: screenshot (0,0) is the top-left of the native desktop
// union, and each axis scales independently to the delivered image size.
func screenshotPointFromNative(desktop frame, deliveredW, deliveredH int, nativeX, nativeY float64) (float64, float64, error) {
	if deliveredW <= 0 || deliveredH <= 0 || desktop.Width <= 0 || desktop.Height <= 0 {
		return 0, 0, fmt.Errorf("desktop screenshot has no coordinate_space/width/height")
	}
	x := (nativeX-desktop.X+0.5)*float64(deliveredW)/desktop.Width - 0.5
	y := (nativeY-desktop.Y+0.5)*float64(deliveredH)/desktop.Height - 0.5
	if x < 0 || y < 0 || x > float64(deliveredW-1) || y > float64(deliveredH-1) {
		return 0, 0, fmt.Errorf("native point (%v, %v) is outside the current %dx%d GUI frame", nativeX, nativeY, deliveredW, deliveredH)
	}
	return x, y, nil
}

type estateGUIError struct {
	path, message string
}

func (e *estateGUIError) Error() string { return e.message }

func estateBatchNeedsSnapshot(actions any) bool {
	items, ok := actions.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		action, _ := item.(map[string]any)
		if isEstateGUIClick(action) {
			return true
		}
	}
	return false
}

// estateBatchIsGeometryOnly is true when every coordinate action is an fx/fy
// window click. Those map from live AX frames, so the last PNG's frame_id
// must not stale the click when unrelated windows move.
func estateBatchIsGeometryOnly(actions any) bool {
	if !estateBatchNeedsSnapshot(actions) {
		return false
	}
	items, _ := actions.([]any)
	for _, item := range items {
		action, _ := item.(map[string]any)
		if action == nil {
			continue
		}
		if isEstateGUIClick(action) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(action["type"]))) {
		case "click", "double_click", "move", "drag", "scroll":
			return false
		}
	}
	return true
}

func stripEstateGUIFields(action map[string]any) map[string]any {
	if action == nil {
		return action
	}
	if _, hasRef := action["window_ref"]; !hasRef {
		if _, hasTitle := action["title"]; !hasTitle {
			if _, hasFX := action["fx"]; !hasFX {
				if _, hasFY := action["fy"]; !hasFY {
					return action
				}
			}
		}
	}
	out := make(map[string]any, len(action))
	for key, value := range action {
		switch key {
		case "window_ref", "title", "fx", "fy":
			continue
		default:
			out[key] = value
		}
	}
	return out
}

func isEstateGUIClick(action map[string]any) bool {
	if action == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(action["type"]))) {
	case "click", "double_click", "move":
	default:
		return false
	}
	_, hasFX := action["fx"]
	_, hasFY := action["fy"]
	return hasFX || hasFY
}

func (s *Service) bindEstateGUIClicks(actions any) (any, error) {
	items, ok := actions.([]any)
	if !ok {
		return actions, nil
	}
	var inventory *axWindowInventory
	out := make([]any, len(items))
	for index, item := range items {
		action, ok := item.(map[string]any)
		if !ok {
			out[index] = item
			continue
		}
		if !isEstateGUIClick(action) {
			out[index] = stripEstateGUIFields(action)
			continue
		}
		if inventory == nil {
			listed, failure := s.listDesktopWindows(true)
			if listed == nil {
				return nil, &estateGUIError{
					path:    fmt.Sprintf("actions[%d]", index),
					message: "the window list could not be read: " + firstLine(ResultText(failure)),
				}
			}
			inventory = listed
		}
		rewritten, err := s.rewriteEstateGUIClick(inventory, action)
		if err != nil {
			if eg, ok := err.(*estateGUIError); ok && eg.path == "" {
				eg.path = fmt.Sprintf("actions[%d]", index)
			}
			return nil, err
		}
		out[index] = rewritten
	}
	return out, nil
}

func (s *Service) rewriteEstateGUIClick(inventory *axWindowInventory, action map[string]any) (map[string]any, error) {
	window, err := estateWindowFromAction(inventory, action)
	if err != nil {
		return nil, err
	}
	if window.bounds == nil || window.bounds.Width <= 0 || window.bounds.Height <= 0 {
		return nil, &estateGUIError{
			path:    "window_ref",
			message: "window " + window.ref + " has no estate window frame",
		}
	}
	fx, ok := mapFloat(action["fx"])
	if !ok {
		return nil, &estateGUIError{path: "fx", message: "estate click requires fx"}
	}
	fy, ok := mapFloat(action["fy"])
	if !ok {
		return nil, &estateGUIError{path: "fy", message: "estate click requires fy"}
	}
	nativeX, nativeY, err := estateNativeClick(*window.bounds, fx, fy)
	if err != nil {
		return nil, &estateGUIError{message: err.Error()}
	}
	desktop, err := desktopFrameFromSnapshot(s.guiSnapshot)
	if err != nil {
		return nil, &estateGUIError{message: err.Error()}
	}
	deliveredW, deliveredH := guiDeliveredSize(desktop)
	x, y, err := screenshotPointFromNative(desktop, deliveredW, deliveredH, nativeX, nativeY)
	if err != nil {
		return nil, &estateGUIError{message: err.Error()}
	}
	rewritten := map[string]any{"type": action["type"], "x": x, "y": y}
	if button, ok := action["button"]; ok {
		rewritten["button"] = button
	}
	if keys, ok := action["keys"]; ok {
		rewritten["keys"] = keys
	}
	return rewritten, nil
}

func estateWindowFromAction(inventory *axWindowInventory, action map[string]any) (*axDesktopWindow, error) {
	if ref, _ := action["window_ref"].(string); strings.TrimSpace(ref) != "" {
		for i := range inventory.windows {
			if inventory.windows[i].ref == ref {
				return &inventory.windows[i], nil
			}
		}
		return nil, &estateGUIError{
			path:    "window_ref",
			message: fmt.Sprintf("window_ref %q is not a current wN from AX apps", ref),
		}
	}
	title, _ := action["title"].(string)
	matches := windowsMatchingDirective(inventory, title)
	switch len(matches) {
	case 1:
		return &matches[0], nil
	case 0:
		return nil, &estateGUIError{
			path:    "title",
			message: "no visible window title or unique app name contains " + strconvQuote(title),
		}
	default:
		return nil, &estateGUIError{
			path:    "title",
			message: fmt.Sprintf("%d windows match title %q; use one wN", len(matches), title),
		}
	}
}

func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func desktopFrameFromSnapshot(snapshot *AppSnapshot) (frame, error) {
	if snapshot == nil {
		return frame{}, fmt.Errorf("GUI coordinate/window actions require a prior GUI screenshot")
	}
	desktop, _ := snapshot.DesktopLayout["desktop"].(map[string]any)
	if desktop == nil {
		return frame{}, fmt.Errorf("GUI screenshot has no desktop frame")
	}
	x, okX := mapFloat(desktop["x"])
	y, okY := mapFloat(desktop["y"])
	width, okW := mapFloat(desktop["width"])
	height, okH := mapFloat(desktop["height"])
	if !okX || !okY || !okW || !okH || width <= 0 || height <= 0 {
		return frame{}, fmt.Errorf("GUI screenshot desktop frame is incomplete")
	}
	return frame{X: x, Y: y, Width: width, Height: height}, nil
}

func mapFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false
		}
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
