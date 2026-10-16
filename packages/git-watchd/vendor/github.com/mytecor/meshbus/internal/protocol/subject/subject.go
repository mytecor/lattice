// Package subject validates and matches meshbus event subjects.
//
// Keeping this grammar below the public API lets routing, wire decoding, and
// interest tracking share one definition without depending on orchestration.
package subject

import (
	"errors"
	"fmt"
	"strings"
)

const MaxBytes = 128

var (
	ErrInvalidTopic   = errors.New("invalid event topic")
	ErrInvalidPattern = errors.New("invalid subscription pattern")
)

func ValidateTopic(topic string) error {
	if err := validateShape(topic, ErrInvalidTopic); err != nil {
		return err
	}
	for _, character := range []byte(topic) {
		if isLiteralByte(character) || character == '.' {
			continue
		}
		return fmt.Errorf("%w: unsupported byte %q", ErrInvalidTopic, character)
	}
	return nil
}

func ValidatePattern(pattern string) error {
	if err := validateShape(pattern, ErrInvalidPattern); err != nil {
		return err
	}
	segments := strings.Split(pattern, ".")
	for index, segment := range segments {
		switch segment {
		case "*":
			continue
		case ">":
			if index != len(segments)-1 {
				return fmt.Errorf("%w: > must be the final segment", ErrInvalidPattern)
			}
			continue
		}
		for _, character := range []byte(segment) {
			if !isLiteralByte(character) {
				return fmt.Errorf("%w: unsupported byte %q", ErrInvalidPattern, character)
			}
		}
	}
	return nil
}

func Match(pattern, topic string) bool {
	patternSegments := strings.Split(pattern, ".")
	topicSegments := strings.Split(topic, ".")
	for index, segment := range patternSegments {
		if segment == ">" {
			return index < len(topicSegments) || (index == 0 && len(patternSegments) == 1)
		}
		if index >= len(topicSegments) || (segment != "*" && segment != topicSegments[index]) {
			return false
		}
	}
	return len(patternSegments) == len(topicSegments)
}

func validateShape(value string, kind error) error {
	if len(value) == 0 || len(value) > MaxBytes || strings.HasPrefix(value, ".") ||
		strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return fmt.Errorf("%w: expected 1-%d bytes in dot-separated segments", kind, MaxBytes)
	}
	return nil
}

func isLiteralByte(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9') || character == '_' || character == '-'
}
