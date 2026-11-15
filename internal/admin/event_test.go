package admin

import (
	"testing"

	"github.com/axin7/faultberth/internal/store"
)

func TestEventViewsShowsNewestFrameFirst(t *testing.T) {
	events := []store.Event{{Data: `{"exception":{"values":[{"type":"TypeError","value":"bad input","stacktrace":{"frames":[{"function":"first"},{"function":"last","filename":"app.go","lineno":12,"in_app":true}]}}]}}`}}
	views := eventViews(events)
	if len(views) != 1 || views[0].Exception != "TypeError: bad input" || len(views[0].Frames) != 2 ||
		views[0].Frames[0].Function != "last" || views[0].Frames[0].Line != 12 || !views[0].Frames[0].InApp {
		t.Fatalf("unexpected event view: %+v", views)
	}
	if got := eventViews([]store.Event{{Data: "invalid"}}); len(got) != 1 || len(got[0].Frames) != 0 {
		t.Fatal("invalid event data should remain available without a parsed stack trace")
	}
}
