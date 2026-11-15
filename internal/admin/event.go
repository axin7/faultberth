package admin

import (
	"encoding/json"
	"strings"

	"github.com/axin7/faultberth/internal/store"
)

type eventView struct {
	store.Event
	Exception string
	Frames    []eventFrame
}

type eventFrame struct {
	Function string `json:"function"`
	Filename string `json:"filename"`
	Module   string `json:"module"`
	Line     int    `json:"lineno"`
	InApp    bool   `json:"in_app"`
}

func eventViews(events []store.Event) []eventView {
	views := make([]eventView, 0, len(events))
	for _, event := range events {
		view := eventView{Event: event}
		var payload struct {
			Exception struct {
				Values []struct {
					Type       string `json:"type"`
					Value      string `json:"value"`
					Stacktrace struct {
						Frames []eventFrame `json:"frames"`
					} `json:"stacktrace"`
				} `json:"values"`
			} `json:"exception"`
		}
		if json.Unmarshal([]byte(event.Data), &payload) == nil && len(payload.Exception.Values) > 0 {
			last := payload.Exception.Values[len(payload.Exception.Values)-1]
			view.Exception = strings.Trim(strings.Join([]string{last.Type, last.Value}, ": "), ": ")
			for i := len(last.Stacktrace.Frames) - 1; i >= 0; i-- {
				view.Frames = append(view.Frames, last.Stacktrace.Frames[i])
			}
		}
		views = append(views, view)
	}
	return views
}
