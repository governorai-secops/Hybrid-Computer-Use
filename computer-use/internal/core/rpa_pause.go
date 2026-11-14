package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	rpaPauseSchema     = "ggaix.rpa-pause.v1"
	rpaPausePathEnv    = "GGAIX_RPA_PAUSE_PATH"
	rpaPauseDefaultRel = ".ggaix/config/rpa-pause.json"
	rpaPauseStateRel   = ".ggaix/state/rpa_global_pause.json"
)

type rpaPauseFile struct {
	Schema         string   `json:"schema"`
	Paused         *bool    `json:"paused"`
	State          string   `json:"state"`
	Reason         string   `json:"reason"`
	ExpiresAtEpoch *float64 `json:"expires_at_epoch"`
}

func estateRPAPause() (paused bool, reason string) {
	if path := strings.TrimSpace(os.Getenv(rpaPausePathEnv)); path != "" {
		return evaluateRPAPauseFile(path, true, time.Now().Unix())
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return true, "RPA_PAUSE_HOME_UNAVAILABLE"
	}
	paused, reason = evaluateRPAPauseFile(filepath.Join(home, rpaPauseDefaultRel), true, time.Now().Unix())
	if paused {
		return paused, reason
	}
	statePaused, stateReason := evaluateRPAPauseFile(filepath.Join(home, rpaPauseStateRel), false, time.Now().Unix())
	if statePaused {
		return true, stateReason
	}
	return false, reason
}

func evaluateRPAPauseFile(path string, strictSchema bool, nowEpoch int64) (bool, string) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "ACTIVE"
		}
		return true, "RPA_PAUSE_STATE_INVALID:" + err.Error()
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, "RPA_PAUSE_STATE_SYMLINK_FAIL_CLOSED"
	}
	if !info.Mode().IsRegular() {
		return true, "RPA_PAUSE_STATE_NOT_REGULAR_FILE"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return true, "RPA_PAUSE_STATE_INVALID:" + err.Error()
	}
	var file rpaPauseFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return true, "RPA_PAUSE_STATE_INVALID:JSON"
	}
	if strictSchema && file.Schema != rpaPauseSchema {
		return true, "RPA_PAUSE_SCHEMA_INVALID_FAIL_CLOSED"
	}
	var paused bool
	switch {
	case file.Paused != nil:
		paused = *file.Paused
	case strings.EqualFold(file.State, "PAUSED"), strings.EqualFold(file.State, "UNVERIFIED"):
		paused = true
	case strings.EqualFold(file.State, "ACTIVE"):
		paused = false
	default:
		if strictSchema {
			return true, "RPA_PAUSE_SCHEMA_INVALID_FAIL_CLOSED"
		}
		return true, "RPA_PAUSE_MISSING_BOOL:" + filepath.Base(path)
	}
	reason := strings.TrimSpace(file.Reason)
	if reason == "" {
		if paused {
			reason = "OPERATOR_PAUSED"
		} else {
			reason = "ACTIVE"
		}
	}
	if paused && file.ExpiresAtEpoch != nil {
		if float64(nowEpoch) >= *file.ExpiresAtEpoch {
			return false, "PAUSE_TTL_EXPIRED:" + reason
		}
	}
	return paused, reason
}
