package client

import (
	"strings"

	"github.com/google/uuid"
)

type ExtraGenerator func() string

func UuidV4Generator() ExtraGenerator {
	return func() string {
		return uuid.NewString()
	}
}

func IsCommand(text string) bool {
	if i := strings.Index(text, "/"); i == 0 {
		return true
	}
	return false
}

func CheckCommand(text string, entities []*TextEntity) string {
	if IsCommand(text) {
		var cmd string

		// e.g. ["/hello 123", "/hell o 123"]
		// Result: "/hello", "/hell"
		if i := strings.Index(text, " "); i != -1 {
			cmd = text[:i]
		}

		// e.g.: ["/hello@world_bot", "/hello@", "/hello@123"]
		// Result: "/hello"
		if i := strings.Index(text, "@"); i != -1 {
			cmd = text[:i]
		}

		if cmd == "" {
			return text
		}

		return cmd
	}
	return ""
}

func CommandArgument(text string) string {
	if IsCommand(text) {
		// e.g. ["/hello 123", "/hell o 123"]
		// Result: "123", "o 123"
		if i := strings.Index(text, " "); i != -1 {
			return text[i+1:]
		}
	}
	return ""
}
