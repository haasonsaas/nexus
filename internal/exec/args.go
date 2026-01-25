package exec

import (
	"errors"
	"strings"
)

// Argument validation errors.
var (
	ErrEmptyArgument         = errors.New("argument is empty")
	ErrArgumentNullByte      = errors.New("argument contains null byte")
	ErrArgumentControlChar   = errors.New("argument contains control characters")
	ErrArgumentShellMetachar = errors.New("argument contains shell metacharacters")
)

// IsSafeArgument validates that a command argument is safe to use.
// This is less strict than IsSafeExecutableValue because arguments
// can legitimately contain more characters. However, it still checks for:
// 1. Empty values (rejected)
// 2. Null bytes (rejected)
// 3. Control characters like newlines (rejected)
// 4. Shell metacharacters ;&|`$<> (rejected)
//
// Note: Unlike executable values, arguments can start with - and contain quotes,
// as these are common in legitimate command arguments.
func IsSafeArgument(arg string) bool {
	if arg == "" {
		return false
	}

	// Check for null bytes
	if strings.Contains(arg, "\x00") {
		return false
	}

	// Check for control characters (newlines, carriage returns)
	if ControlChars.MatchString(arg) {
		return false
	}

	// Check for shell metacharacters
	if ShellMetachars.MatchString(arg) {
		return false
	}

	return true
}

// SanitizeArgument validates a single argument and returns it if safe.
// Returns an error describing why the argument is unsafe if validation fails.
func SanitizeArgument(arg string) (string, error) {
	if arg == "" {
		return "", ErrEmptyArgument
	}

	// Check for null bytes
	if strings.Contains(arg, "\x00") {
		return "", ErrArgumentNullByte
	}

	// Check for control characters (newlines, carriage returns)
	if ControlChars.MatchString(arg) {
		return "", ErrArgumentControlChar
	}

	// Check for shell metacharacters
	if ShellMetachars.MatchString(arg) {
		return "", ErrArgumentShellMetachar
	}

	return arg, nil
}

// SanitizeArguments validates a slice of arguments and returns them if all are safe.
// Returns an error if any argument fails validation.
func SanitizeArguments(args []string) ([]string, error) {
	if args == nil {
		return nil, nil
	}

	result := make([]string, 0, len(args))
	for i, arg := range args {
		sanitized, err := SanitizeArgument(arg)
		if err != nil {
			return nil, &ArgumentError{Index: i, Arg: arg, Err: err}
		}
		result = append(result, sanitized)
	}

	return result, nil
}

// ArgumentError provides context about which argument failed validation.
type ArgumentError struct {
	Index int
	Arg   string
	Err   error
}

func (e *ArgumentError) Error() string {
	return "argument " + string(rune('0'+e.Index)) + " is unsafe: " + e.Err.Error()
}

func (e *ArgumentError) Unwrap() error {
	return e.Err
}
