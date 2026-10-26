package handler

import (
	"encoding/json"
	"reflect"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUserResponseSuppressesSourcePrompt(t *testing.T) {
	for _, tc := range []struct {
		name, stored, want string
	}{
		{"missing", "", `{"source_skipped":true}`},
		{"null", "null", `{"source_skipped":true}`},
		{"empty", `{}`, `{"source_skipped":true}`},
		{"unanswered", `{"source":[],"source_skipped":false,"role":"engineer","use_case":["ship_code"],"version":2}`, `{"source_skipped":true,"role":"engineer","use_case":["ship_code"],"version":2}`},
		{"answered", `{"source":["other"],"source_other":"old answer","future_field":"preserved"}`, `{"future_field":"preserved","source_skipped":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := db.User{OnboardingQuestionnaire: []byte(tc.stored)}
			response := (&Handler{}).userToResponse(user)
			var got, want map[string]any
			if err := json.Unmarshal(response.OnboardingQuestionnaire, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("questionnaire = %s, want %s", response.OnboardingQuestionnaire, tc.want)
			}
			if string(user.OnboardingQuestionnaire) != tc.stored {
				t.Fatal("response changed stored questionnaire")
			}
		})
	}
}
