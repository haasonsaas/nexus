package index

import (
	"sync"

	"github.com/haasonsaas/nexus/internal/rag/parser/markdown"
	"github.com/haasonsaas/nexus/internal/rag/parser/text"
)

var registerParsersOnce sync.Once

func ensureDefaultParsers() {
	registerParsersOnce.Do(func() {
		markdown.Register()
		text.Register()
	})
}
