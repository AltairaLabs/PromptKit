package variables

import (
	"context"
	"testing"
	"time"
)

func TestTimeProvider_Name(t *testing.T) {
	p := NewTimeProvider()
	if got := p.Name(); got != "time" {
		t.Errorf("TimeProvider.Name() = %v, want %v", got, "time")
	}
}

func TestTimeProvider_Provide(t *testing.T) {
	// Fixed time for testing: Monday, January 15, 2024, 14:30:00 UTC
	fixedTime := time.Date(2024, time.January, 15, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		provider *TimeProvider
		want     map[string]string
	}{
		{
			name:     "default provider",
			provider: NewTimeProvider().WithNowFunc(func() time.Time { return fixedTime }),
			want: map[string]string{
				"current_time":    "2024-01-15T14:30:00Z",
				"current_date":    "2024-01-15",
				"current_year":    "2024",
				"current_month":   "January",
				"current_weekday": "Monday",
				"current_hour":    "14",
			},
		},
		{
			name: "custom format",
			provider: NewTimeProviderWithFormat("2006/01/02 15:04").
				WithNowFunc(func() time.Time { return fixedTime }),
			want: map[string]string{
				"current_time":    "2024/01/15 14:30",
				"current_date":    "2024-01-15",
				"current_year":    "2024",
				"current_month":   "January",
				"current_weekday": "Monday",
				"current_hour":    "14",
			},
		},
		{
			name: "with timezone",
			provider: NewTimeProviderWithLocation(time.FixedZone("EST", -5*60*60)).
				WithNowFunc(func() time.Time { return fixedTime }),
			want: map[string]string{
				"current_time":    "2024-01-15T09:30:00-05:00",
				"current_date":    "2024-01-15",
				"current_year":    "2024",
				"current_month":   "January",
				"current_weekday": "Monday",
				"current_hour":    "09",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.provider.Provide(context.Background())
			if err != nil {
				t.Errorf("TimeProvider.Provide() error = %v", err)
				return
			}

			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("TimeProvider.Provide()[%s] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestTimeProvider_NoExternalDeps(t *testing.T) {
	fixedTime := time.Date(2024, time.January, 15, 14, 30, 0, 0, time.UTC)
	p := NewTimeProvider().WithNowFunc(func() time.Time { return fixedTime })

	// Provider works without any external dependencies
	got, err := p.Provide(context.Background())
	if err != nil {
		t.Errorf("TimeProvider.Provide() error = %v", err)
		return
	}

	if got["current_time"] != "2024-01-15T14:30:00Z" {
		t.Errorf("TimeProvider.Provide()[current_time] = %v, want %v", got["current_time"], "2024-01-15T14:30:00Z")
	}
}
