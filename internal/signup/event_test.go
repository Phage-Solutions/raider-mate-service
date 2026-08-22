package signup

import (
	"testing"
	"time"

	"github.com/Phage-Solutions/raider-mate-service/internal/db"
)

// The bot records the post it just made through the same PATCH path a raid lead edits
// through, so this predicate is what keeps an announcement from redrawing itself.
func TestChangesWhatIsPostedIgnoresTheBotRecordingItsOwnMessage(t *testing.T) {
	messageID := int64(999)
	channelID := int64(555)

	in := UpdateEventInput{MessageID: &messageID, ChannelID: &channelID}

	if in.changesWhatIsPosted() {
		t.Fatal("recording a message id asked for a redraw of the message it just recorded")
	}
}

func TestChangesWhatIsPostedCoversEveryFieldARaiderReads(t *testing.T) {
	title := "Ulduar"
	at := time.Now()
	difficulty := db.RaidDifficultyMYTHIC
	lead := int32(30)
	report := "https://www.warcraftlogs.com/reports/abc"

	cases := map[string]UpdateEventInput{
		"title":                 {Title: &title},
		"starts_at":             {StartsAt: &at},
		"signup_deadline":       {SignupDeadline: &at},
		"comp_template":         {CompTemplate: []byte(`{"tanks":2}`)},
		"difficulty":            {Difficulty: &difficulty},
		"reminder_lead_minutes": {ReminderLeadMinutes: &lead},
		"warcraftlogs_url":      {WarcraftLogsURL: &report},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if !in.changesWhatIsPosted() {
				t.Fatalf("an edit to %s left the sheet in the channel showing the old one", name)
			}
		})
	}
}

func TestChangesWhatIsPostedIsFalseForAnEmptyEdit(t *testing.T) {
	if (UpdateEventInput{}).changesWhatIsPosted() {
		t.Fatal("an edit that changed nothing asked for a redraw")
	}
}
