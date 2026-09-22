// Package domain contains immutable normative source values and diff rules.
package domain

import (
	"bytes"
	"sort"
	"strings"
	"unicode/utf8"
)

type Source struct {
	ID               string `json:"id"`
	ActCode          string `json:"act_code"`
	Title            string `json:"title"`
	Revision         string `json:"revision"`
	PublishedOn      string `json:"published_on,omitempty"`
	EffectiveOn      string `json:"effective_on"`
	SourceURL        string `json:"source_url"`
	SourceHost       string `json:"source_host"`
	ContentSHA256    string `json:"content_sha256"`
	ContentType      string `json:"content_type"`
	OriginalFilename string `json:"original_filename"`
	SizeBytes        int    `json:"size_bytes"`
	ImportedBy       string `json:"imported_by"`
	ImportedAt       string `json:"imported_at"`
	Content          []byte `json:"-"`
}

type Import struct {
	ActCode          string
	Title            string
	Revision         string
	PublishedOn      string
	EffectiveOn      string
	SourceURL        string
	SourceHost       string
	ExpectedSHA256   string
	ContentType      string
	OriginalFilename string
	ImportedBy       string
	Content          []byte
}

type TextChanges struct {
	Added     []string `json:"added,omitempty"`
	Removed   []string `json:"removed,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

type DiffProtocol struct {
	ActCode      string       `json:"act_code"`
	FromID       string       `json:"from_id"`
	FromRevision string       `json:"from_revision"`
	FromSHA256   string       `json:"from_sha256"`
	FromSize     int          `json:"from_size_bytes"`
	ToID         string       `json:"to_id"`
	ToRevision   string       `json:"to_revision"`
	ToSHA256     string       `json:"to_sha256"`
	ToSize       int          `json:"to_size_bytes"`
	Changed      bool         `json:"changed"`
	TextChanges  *TextChanges `json:"text_changes,omitempty"`
}

func Compare(from, to Source) DiffProtocol {
	protocol := DiffProtocol{
		ActCode: from.ActCode, FromID: from.ID, FromRevision: from.Revision,
		FromSHA256: from.ContentSHA256, FromSize: from.SizeBytes,
		ToID: to.ID, ToRevision: to.Revision, ToSHA256: to.ContentSHA256,
		ToSize: to.SizeBytes, Changed: from.ContentSHA256 != to.ContentSHA256,
	}
	if !textual(from.Content) || !textual(to.Content) {
		return protocol
	}
	oldLines, newLines := lineSet(from.Content), lineSet(to.Content)
	added := make([]string, 0)
	for line := range newLines {
		if !oldLines[line] {
			added = append(added, line)
		}
	}
	removed := make([]string, 0)
	for line := range oldLines {
		if !newLines[line] {
			removed = append(removed, line)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	changes := &TextChanges{
		Added:     limitedLines(added),
		Removed:   limitedLines(removed),
		Truncated: len(added) > maxProtocolLines || len(removed) > maxProtocolLines,
	}
	protocol.TextChanges = changes
	return protocol
}

func textual(content []byte) bool {
	return utf8.Valid(content) && !bytes.Contains(content, []byte{0})
}

func lineSet(content []byte) map[string]bool {
	result := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result[line] = true
		}
	}
	return result
}

const maxProtocolLines = 200

func limitedLines(lines []string) []string {
	if len(lines) <= maxProtocolLines {
		return lines
	}
	return lines[:maxProtocolLines]
}
