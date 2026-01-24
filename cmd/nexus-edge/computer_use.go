package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/tools/computeruse"
)

type computerUseParams struct {
	Action          string  `json:"action"`
	Coordinate      []int   `json:"coordinate"`
	StartCoordinate []int   `json:"start_coordinate"`
	EndCoordinate   []int   `json:"end_coordinate"`
	Text            string  `json:"text"`
	ScrollDirection string  `json:"scroll_direction"`
	ScrollAmount    int     `json:"scroll_amount"`
	DurationMs      int     `json:"duration_ms"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// computerUseTool performs direct mouse/keyboard/screenshot actions for computer use.
func computerUseTool(policy *ComputerUsePolicy) *Tool {
	return &Tool{
		Name:              "nodes.computer_use",
		Description:       "Perform computer-use actions (mouse, keyboard, scroll, screenshot). Intended for Claude computer use.",
		InputSchema:       computeruse.SchemaJSON,
		RequiresApproval:  true,
		TimeoutSeconds:    60,
		ProducesArtifacts: true,
		Handler: func(ctx context.Context, input string) (*ToolResult, error) {
			return handleComputerUse(ctx, input, policy)
		},
	}
}

func handleComputerUse(ctx context.Context, input string, policy *ComputerUsePolicy) (*ToolResult, error) {
	var params computerUseParams
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	action := strings.ToLower(strings.TrimSpace(params.Action))
	if action == "" {
		return &ToolResult{Content: "action is required", IsError: true}, nil
	}
	if !computerUseActionAllowed(policy, action) {
		return &ToolResult{Content: "action denied by computer_use policy", IsError: true}, nil
	}

	switch action {
	case "screenshot":
		return handleScreenCapture(ctx, "{}")
	case "wait":
		wait := time.Duration(params.DurationMs) * time.Millisecond
		if wait <= 0 && params.DurationSeconds > 0 {
			wait = time.Duration(params.DurationSeconds * float64(time.Second))
		}
		if wait <= 0 {
			wait = 500 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return &ToolResult{Content: "wait cancelled", IsError: true}, nil
		case <-time.After(wait):
			return &ToolResult{Content: fmt.Sprintf("waited %s", wait)}, nil
		}
	case "cursor_position":
		return handleCursorPosition(ctx)
	default:
		return handleComputerInputAction(ctx, action, params)
	}
}

func handleCursorPosition(ctx context.Context) (*ToolResult, error) {
	switch runtime.GOOS {
	case "darwin":
		return runMacComputerAction(ctx, map[string]any{
			"action": "cursor_position",
		})
	case "linux":
		return runLinuxComputerAction(ctx, "cursor_position", computerUseParams{})
	default:
		return &ToolResult{Content: fmt.Sprintf("cursor_position not supported on %s", runtime.GOOS), IsError: true}, nil
	}
}

func handleComputerInputAction(ctx context.Context, action string, params computerUseParams) (*ToolResult, error) {
	switch runtime.GOOS {
	case "darwin":
		payload := map[string]any{
			"action": action,
		}
		if len(params.Coordinate) >= 2 {
			payload["coordinate"] = params.Coordinate[:2]
		}
		if len(params.StartCoordinate) >= 2 {
			payload["start_coordinate"] = params.StartCoordinate[:2]
		}
		if len(params.EndCoordinate) >= 2 {
			payload["end_coordinate"] = params.EndCoordinate[:2]
		}
		if params.Text != "" {
			payload["text"] = params.Text
		}
		if params.ScrollDirection != "" {
			payload["scroll_direction"] = params.ScrollDirection
		}
		if params.ScrollAmount != 0 {
			payload["scroll_amount"] = params.ScrollAmount
		}
		if params.DurationMs > 0 {
			payload["duration_ms"] = params.DurationMs
		}
		return runMacComputerAction(ctx, payload)
	case "linux":
		return runLinuxComputerAction(ctx, action, params)
	default:
		return &ToolResult{Content: fmt.Sprintf("computer_use not supported on %s", runtime.GOOS), IsError: true}, nil
	}
}

func computerUseActionAllowed(policy *ComputerUsePolicy, action string) bool {
	if policy == nil {
		return true
	}
	action = strings.ToLower(strings.TrimSpace(action))
	for _, deny := range policy.Denylist {
		if action == strings.ToLower(strings.TrimSpace(deny)) {
			return false
		}
	}
	if len(policy.Allowlist) == 0 {
		return true
	}
	for _, allow := range policy.Allowlist {
		if action == strings.ToLower(strings.TrimSpace(allow)) {
			return true
		}
	}
	return false
}

var computerUseSwiftOnce sync.Once
var computerUseSwiftPath string
var computerUseSwiftErr error

func runMacComputerAction(ctx context.Context, payload map[string]any) (*ToolResult, error) {
	if _, err := exec.LookPath("swift"); err != nil {
		return &ToolResult{Content: "computer_use on macOS requires swift (Xcode Command Line Tools)", IsError: true}, nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode input: %w", err)
	}

	computerUseSwiftOnce.Do(func() {
		path := filepath.Join(os.TempDir(), "nexus_computer_use.swift")
		if writeErr := os.WriteFile(path, []byte(macComputerUseScript), 0o600); writeErr != nil {
			computerUseSwiftErr = fmt.Errorf("write swift helper: %w", writeErr)
			return
		}
		computerUseSwiftPath = path
	})
	if computerUseSwiftErr != nil {
		return nil, computerUseSwiftErr
	}

	cmd := exec.CommandContext(ctx, "swift", computerUseSwiftPath)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = append(os.Environ(), fmt.Sprintf("NEXUS_DISPLAY_NUMBER=%d", displayInfo().DisplayNumber))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("computer_use failed: %v\n%s", err, string(output)), IsError: true}, nil
	}

	content := strings.TrimSpace(string(output))
	if content == "" {
		content = "ok"
	}
	return &ToolResult{Content: content}, nil
}

func runLinuxComputerAction(ctx context.Context, action string, params computerUseParams) (*ToolResult, error) {
	if _, err := exec.LookPath("xdotool"); err != nil {
		return &ToolResult{Content: "computer_use on Linux requires xdotool (apt install xdotool)", IsError: true}, nil
	}

	switch action {
	case "mouse_move":
		if len(params.Coordinate) < 2 {
			return &ToolResult{Content: "coordinate is required for mouse_move", IsError: true}, nil
		}
		return runLinuxCmd(ctx, "xdotool", "mousemove", strconv.Itoa(params.Coordinate[0]), strconv.Itoa(params.Coordinate[1]))
	case "left_click", "right_click", "middle_click", "double_click", "triple_click":
		button := "1"
		clicks := 1
		switch action {
		case "right_click":
			button = "3"
		case "middle_click":
			button = "2"
		case "double_click":
			clicks = 2
		case "triple_click":
			clicks = 3
		}
		for i := 0; i < clicks; i++ {
			if _, err := runLinuxCmd(ctx, "xdotool", "click", button); err != nil {
				return nil, err
			}
		}
		return &ToolResult{Content: "ok"}, nil
	case "scroll":
		amount := params.ScrollAmount
		if amount == 0 {
			amount = 3
		}
		btn := "4"
		switch strings.ToLower(params.ScrollDirection) {
		case "down":
			btn = "5"
		case "left":
			btn = "6"
		case "right":
			btn = "7"
		}
		for i := 0; i < amount; i++ {
			if _, err := runLinuxCmd(ctx, "xdotool", "click", btn); err != nil {
				return nil, err
			}
		}
		return &ToolResult{Content: "ok"}, nil
	case "type":
		if strings.TrimSpace(params.Text) == "" {
			return &ToolResult{Content: "text is required for type", IsError: true}, nil
		}
		return runLinuxCmd(ctx, "xdotool", "type", "--delay", "10", params.Text)
	case "key", "hold_key":
		if strings.TrimSpace(params.Text) == "" {
			return &ToolResult{Content: "text is required for key", IsError: true}, nil
		}
		return runLinuxCmd(ctx, "xdotool", "key", params.Text)
	case "cursor_position":
		cmd := exec.CommandContext(ctx, "xdotool", "getmouselocation", "--shell")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return &ToolResult{Content: fmt.Sprintf("cursor_position failed: %v\n%s", err, string(output)), IsError: true}, nil
		}
		lines := strings.Split(string(output), "\n")
		data := make(map[string]int)
		for _, line := range lines {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			val, convErr := strconv.Atoi(strings.TrimSpace(parts[1]))
			if convErr == nil {
				data[strings.ToLower(strings.TrimSpace(parts[0]))] = val
			}
		}
		payload, _ := json.Marshal(data)
		return &ToolResult{Content: string(payload)}, nil
	default:
		return &ToolResult{Content: fmt.Sprintf("action %q not supported on linux", action), IsError: true}, nil
	}
}

func runLinuxCmd(ctx context.Context, name string, args ...string) (*ToolResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("%s failed: %v\n%s", name, err, string(output)), IsError: true}, nil
	}
	return &ToolResult{Content: strings.TrimSpace(string(output))}, nil
}

func displayInfo() DisplayInfo {
	info, err := loadDisplayInfo()
	if err != nil {
		return DisplayInfo{WidthPx: 0, HeightPx: 0, Scale: 1, DisplayNumber: 0, Count: 1}
	}
	return info
}

type swiftSystemInfo struct {
	DisplayWidthPx      int     `json:"display_width_px"`
	DisplayHeightPx     int     `json:"display_height_px"`
	DisplayScale        float64 `json:"display_scale"`
	DisplayNumber       int     `json:"display_number"`
	DisplayCount        int     `json:"display_count"`
	PermAccessibility   string  `json:"perm_accessibility"`
	PermScreenRecording string  `json:"perm_screen_recording"`
	PermCamera          string  `json:"perm_camera"`
	PermMicrophone      string  `json:"perm_microphone"`
	PermNotifications   string  `json:"perm_notifications"`
}

var displayInfoOnce sync.Once
var displayInfoCache DisplayInfo
var displayInfoErr error

func loadDisplayInfo() (DisplayInfo, error) {
	displayInfoOnce.Do(func() {
		info, err := collectSwiftSystemInfo()
		if err != nil {
			displayInfoErr = err
			return
		}
		displayInfoCache = DisplayInfo{
			WidthPx:       info.DisplayWidthPx,
			HeightPx:      info.DisplayHeightPx,
			Scale:         info.DisplayScale,
			DisplayNumber: info.DisplayNumber,
			Count:         info.DisplayCount,
			Permissions: map[string]string{
				"perm_accessibility":    info.PermAccessibility,
				"perm_screen_recording": info.PermScreenRecording,
				"perm_camera":           info.PermCamera,
				"perm_microphone":       info.PermMicrophone,
				"perm_notifications":    info.PermNotifications,
			},
		}
	})
	return displayInfoCache, displayInfoErr
}

func collectSwiftSystemInfo() (swiftSystemInfo, error) {
	if runtime.GOOS != "darwin" {
		return swiftSystemInfo{}, errors.New("system info only available on darwin")
	}
	if _, err := exec.LookPath("swift"); err != nil {
		return swiftSystemInfo{}, fmt.Errorf("swift unavailable: %w", err)
	}
	cmd := exec.Command("swift", "-e", macSystemInfoScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return swiftSystemInfo{}, fmt.Errorf("system info failed: %v\n%s", err, string(output))
	}
	var info swiftSystemInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return swiftSystemInfo{}, fmt.Errorf("parse system info: %w", err)
	}
	if info.DisplayWidthPx == 0 || info.DisplayHeightPx == 0 {
		return swiftSystemInfo{}, errors.New("display info unavailable")
	}
	return info, nil
}

// DisplayInfo holds the active display configuration and permissions.
type DisplayInfo struct {
	WidthPx       int
	HeightPx      int
	Scale         float64
	DisplayNumber int
	Count         int
	Permissions   map[string]string
}

func computerUseMetadata() map[string]string {
	info, err := loadDisplayInfo()
	if err != nil {
		return nil
	}
	metadata := map[string]string{
		"display_width_px":  strconv.Itoa(info.WidthPx),
		"display_height_px": strconv.Itoa(info.HeightPx),
		"display_scale":     fmt.Sprintf("%.2f", info.Scale),
		"display_number":    strconv.Itoa(info.DisplayNumber),
		"display_count":     strconv.Itoa(info.Count),
	}
	for key, value := range info.Permissions {
		if strings.TrimSpace(value) == "" {
			continue
		}
		metadata[key] = value
	}
	return metadata
}

const macComputerUseScript = `
import AppKit
import ApplicationServices
import Foundation

struct Input: Decodable {
    let action: String
    let coordinate: [Double]?
    let start_coordinate: [Double]?
    let end_coordinate: [Double]?
    let text: String?
    let scroll_direction: String?
    let scroll_amount: Int?
    let duration_ms: Int?
}

func screenForDisplay(number: Int?) -> NSScreen {
    let screens = NSScreen.screens
    if let n = number, n >= 0, n < screens.count {
        return screens[n]
    }
    if let main = NSScreen.main {
        return main
    }
    return screens.first!
}

let displayNumberEnv = ProcessInfo.processInfo.environment["NEXUS_DISPLAY_NUMBER"]
let displayNumber = displayNumberEnv.flatMap { Int($0) }
let screen = screenForDisplay(number: displayNumber)
let scale = screen.backingScaleFactor
let frame = screen.frame

func pointFromPixels(_ coord: [Double]) -> CGPoint {
    let x = frame.minX + (coord[0] / scale)
    let y = frame.minY + (frame.height - (coord[1] / scale))
    return CGPoint(x: x, y: y)
}

func currentMousePoint() -> CGPoint {
    return NSEvent.mouseLocation
}

func postMouse(_ type: CGEventType, point: CGPoint, button: CGMouseButton) {
    let event = CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: point, mouseButton: button)
    event?.post(tap: .cghidEventTap)
}

func click(_ button: CGMouseButton, point: CGPoint, count: Int) {
    for i in 1...count {
        let down = CGEvent(mouseEventSource: nil, mouseType: button == .left ? .leftMouseDown : (button == .right ? .rightMouseDown : .otherMouseDown), mouseCursorPosition: point, mouseButton: button)
        down?.setIntegerValueField(.mouseEventClickState, value: Int64(i))
        down?.post(tap: .cghidEventTap)
        let up = CGEvent(mouseEventSource: nil, mouseType: button == .left ? .leftMouseUp : (button == .right ? .rightMouseUp : .otherMouseUp), mouseCursorPosition: point, mouseButton: button)
        up?.setIntegerValueField(.mouseEventClickState, value: Int64(i))
        up?.post(tap: .cghidEventTap)
    }
}

func parseKeyCombo(_ text: String) -> (CGKeyCode, CGEventFlags)? {
    let parts = text.lowercased().split(separator: "+").map { String($0) }
    var flags = CGEventFlags()
    var keyPart = ""
    for part in parts {
        switch part {
        case "cmd", "command", "meta":
            flags.insert(.maskCommand)
        case "ctrl", "control":
            flags.insert(.maskControl)
        case "alt", "option":
            flags.insert(.maskAlternate)
        case "shift":
            flags.insert(.maskShift)
        case "fn":
            flags.insert(.maskSecondaryFn)
        default:
            keyPart = part
        }
    }
    if keyPart.isEmpty {
        return nil
    }
    let codes: [String: CGKeyCode] = [
        "a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9,
        "b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17,
        "1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "=": 24, "9": 25, "7": 26,
        "-": 27, "8": 28, "0": 29, "]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35,
        "l": 37, "j": 38, "'": 39, "k": 40, ";": 41, "\\\\": 42, ",": 43, "/": 44, "n": 45,
        "m": 46, ".": 47, "tab": 48, "space": 49, "return": 36, "enter": 76, "escape": 53,
        "esc": 53, "delete": 51, "backspace": 51, "forward_delete": 117,
        "left": 123, "right": 124, "down": 125, "up": 126,
        "home": 115, "end": 119, "pageup": 116, "pagedown": 121,
        "f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
        "f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111
    ]
    if let code = codes[keyPart] {
        return (code, flags)
    }
    if keyPart.count == 1, let char = keyPart.first {
        let key = String(char)
        if let code = codes[key] {
            return (code, flags)
        }
    }
    return nil
}

func sendKey(_ combo: String, holdMs: Int?) -> Bool {
    guard let (keyCode, flags) = parseKeyCombo(combo) else {
        return false
    }
    let down = CGEvent(keyboardEventSource: nil, virtualKey: keyCode, keyDown: true)
    down?.flags = flags
    down?.post(tap: .cghidEventTap)
    if let hold = holdMs, hold > 0 {
        usleep(useconds_t(hold * 1000))
    }
    let up = CGEvent(keyboardEventSource: nil, virtualKey: keyCode, keyDown: false)
    up?.flags = flags
    up?.post(tap: .cghidEventTap)
    return true
}

func sendText(_ text: String) {
    let event = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true)
    let scalars = Array(text.utf16)
    event?.keyboardSetUnicodeString(stringLength: scalars.count, unicodeString: scalars)
    event?.post(tap: .cghidEventTap)
    let up = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false)
    up?.keyboardSetUnicodeString(stringLength: scalars.count, unicodeString: scalars)
    up?.post(tap: .cghidEventTap)
}

let inputData = FileHandle.standardInput.readDataToEndOfFile()
let input = try JSONDecoder().decode(Input.self, from: inputData)
let action = input.action.lowercased()

if action != "cursor_position" && !AXIsProcessTrusted() {
    fputs("accessibility permission required\n", stderr)
    exit(2)
}

switch action {
case "mouse_move":
    guard let coord = input.coordinate, coord.count >= 2 else {
        fputs("coordinate required\n", stderr)
        exit(1)
    }
    let point = pointFromPixels(coord)
    postMouse(.mouseMoved, point: point, button: .left)
case "left_click":
    guard let coord = input.coordinate, coord.count >= 2 else {
        fputs("coordinate required\n", stderr)
        exit(1)
    }
    click(.left, point: pointFromPixels(coord), count: 1)
case "right_click":
    guard let coord = input.coordinate, coord.count >= 2 else {
        fputs("coordinate required\n", stderr)
        exit(1)
    }
    click(.right, point: pointFromPixels(coord), count: 1)
case "middle_click":
    guard let coord = input.coordinate, coord.count >= 2 else {
        fputs("coordinate required\n", stderr)
        exit(1)
    }
    click(.center, point: pointFromPixels(coord), count: 1)
case "double_click":
    guard let coord = input.coordinate, coord.count >= 2 else {
        fputs("coordinate required\n", stderr)
        exit(1)
    }
    click(.left, point: pointFromPixels(coord), count: 2)
case "triple_click":
    guard let coord = input.coordinate, coord.count >= 2 else {
        fputs("coordinate required\n", stderr)
        exit(1)
    }
    click(.left, point: pointFromPixels(coord), count: 3)
case "left_mouse_down":
    let point = input.coordinate != nil && input.coordinate!.count >= 2 ? pointFromPixels(input.coordinate!) : currentMousePoint()
    postMouse(.leftMouseDown, point: point, button: .left)
case "left_mouse_up":
    let point = input.coordinate != nil && input.coordinate!.count >= 2 ? pointFromPixels(input.coordinate!) : currentMousePoint()
    postMouse(.leftMouseUp, point: point, button: .left)
case "left_click_drag":
    let startPoint = input.start_coordinate != nil && input.start_coordinate!.count >= 2 ? pointFromPixels(input.start_coordinate!) : currentMousePoint()
    guard let endCoord = input.end_coordinate ?? input.coordinate, endCoord.count >= 2 else {
        fputs("coordinate required\n", stderr)
        exit(1)
    }
    let endPoint = pointFromPixels(endCoord)
    postMouse(.leftMouseDown, point: startPoint, button: .left)
    postMouse(.leftMouseDragged, point: endPoint, button: .left)
    postMouse(.leftMouseUp, point: endPoint, button: .left)
case "scroll":
    let amount = input.scroll_amount ?? 3
    let direction = (input.scroll_direction ?? "down").lowercased()
    var vertical = 0
    var horizontal = 0
    switch direction {
    case "up":
        vertical = amount
    case "down":
        vertical = -amount
    case "left":
        horizontal = -amount
    case "right":
        horizontal = amount
    default:
        vertical = -amount
    }
    let event = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 2, wheel1: Int32(vertical), wheel2: Int32(horizontal), wheel3: 0)
    event?.post(tap: .cghidEventTap)
case "type":
    guard let text = input.text else {
        fputs("text required\n", stderr)
        exit(1)
    }
    sendText(text)
case "key":
    guard let text = input.text, sendKey(text, holdMs: nil) else {
        fputs("key required\n", stderr)
        exit(1)
    }
case "hold_key":
    guard let text = input.text else {
        fputs("key required\n", stderr)
        exit(1)
    }
    let hold = input.duration_ms ?? 200
    if !sendKey(text, holdMs: hold) {
        fputs("key required\n", stderr)
        exit(1)
    }
case "cursor_position":
    let loc = NSEvent.mouseLocation
    let xPx = (loc.x - frame.minX) * scale
    let yPx = (frame.height - (loc.y - frame.minY)) * scale
    let payload: [String: Double] = ["x": xPx, "y": yPx]
    let data = try JSONSerialization.data(withJSONObject: payload, options: [])
    if let json = String(data: data, encoding: .utf8) {
        print(json)
        exit(0)
    }
default:
    fputs("unsupported action\n", stderr)
    exit(1)
}

print("{\"status\":\"ok\"}")
`

const macSystemInfoScript = `
import AppKit
import ApplicationServices
import AVFoundation
import UserNotifications
import Foundation

func authString(_ status: AVAuthorizationStatus) -> String {
    switch status {
    case .authorized: return "authorized"
    case .denied: return "denied"
    case .restricted: return "restricted"
    case .notDetermined: return "not_determined"
    @unknown default: return "unknown"
    }
}

let screens = NSScreen.screens
let main = NSScreen.main ?? screens.first
var output: [String: Any] = [:]
output["display_count"] = screens.count
if let screen = main {
    let scale = screen.backingScaleFactor
    let widthPx = Int(screen.frame.width * scale)
    let heightPx = Int(screen.frame.height * scale)
    output["display_width_px"] = widthPx
    output["display_height_px"] = heightPx
    output["display_scale"] = scale
    output["display_number"] = 0
}
output["perm_accessibility"] = AXIsProcessTrusted() ? "granted" : "denied"
output["perm_screen_recording"] = CGPreflightScreenCaptureAccess() ? "granted" : "denied"
output["perm_camera"] = authString(AVCaptureDevice.authorizationStatus(for: .video))
output["perm_microphone"] = authString(AVCaptureDevice.authorizationStatus(for: .audio))

let sem = DispatchSemaphore(value: 0)
var notifyStatus = "unknown"
UNUserNotificationCenter.current().getNotificationSettings { settings in
    switch settings.authorizationStatus {
    case .authorized: notifyStatus = "authorized"
    case .denied: notifyStatus = "denied"
    case .notDetermined: notifyStatus = "not_determined"
    case .provisional: notifyStatus = "provisional"
    case .ephemeral: notifyStatus = "ephemeral"
    @unknown default: notifyStatus = "unknown"
    }
    sem.signal()
}
_ = sem.wait(timeout: .now() + 1.0)
output["perm_notifications"] = notifyStatus

let data = try JSONSerialization.data(withJSONObject: output, options: [])
if let json = String(data: data, encoding: .utf8) {
    print(json)
}
`
