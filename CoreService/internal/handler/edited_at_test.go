package handler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEntryTimestampJSONIsAdditive(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := created.Add(48 * time.Hour)
	edited := created.Add(24 * time.Hour)
	payload, err := json.Marshal(entryDTO{ID: 42, CreatedAt: created, UpdatedAt: updated, EditedAt: edited})
	if err != nil {
		t.Fatal(err)
	}
	// A client compiled before EditedAt existed can still consume the response.
	var legacy struct {
		ID        int
		CreatedAt time.Time
		UpdatedAt time.Time
	}
	if err := json.Unmarshal(payload, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.ID != 42 || !legacy.CreatedAt.Equal(created) || !legacy.UpdatedAt.Equal(updated) {
		t.Fatal("legacy timestamp fields changed")
	}
	var modern entryDTO
	if err := json.Unmarshal(payload, &modern); err != nil {
		t.Fatal(err)
	}
	if !modern.EditedAt.Equal(edited) {
		t.Fatal("EditedAt missing")
	}
}

func TestEditedAtReadsDoNotRequireMigratedColumn(t *testing.T) {
	for _, file := range []string{"entries.go", "chapters.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sql := string(source)
		if !strings.Contains(sql, "to_jsonb(") || !strings.Contains(sql, "AS edit_time") {
			t.Fatalf("%s lacks old-schema fallback", file)
		}
		if strings.Contains(sql, "ORDER BY edited_at") || strings.Contains(sql, "e.edited_at") {
			t.Fatalf("%s directly requires edited_at", file)
		}
	}
}
