package scheduler

import (
	"testing"
	"time"
)

func TestTaskTypeConstants(t *testing.T) {
	cases := []struct {
		name string
		got  TaskType
		want string
	}{
		{"schedule", TypeSchedule, "schedule"},
		{"alarm", TypeAlarm, "alarm"},
		{"calendar", TypeCalendar, "calendar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestBudgetStruct(t *testing.T) {
	b := Budget{
		MaxTurns:  10,
		MaxTokens: 5000,
		MaxWall:   5 * time.Minute,
		MaxUSD:    0.50,
	}

	if b.MaxTurns != 10 {
		t.Errorf("MaxTurns = %d", b.MaxTurns)
	}
	if b.MaxTokens != 5000 {
		t.Errorf("MaxTokens = %d", b.MaxTokens)
	}
	if b.MaxWall != 5*time.Minute {
		t.Errorf("MaxWall = %v", b.MaxWall)
	}
	if b.MaxUSD != 0.50 {
		t.Errorf("MaxUSD = %f", b.MaxUSD)
	}
}

func TestTaskStruct(t *testing.T) {
	now := time.Now()
	task := Task{
		ID:        "task-1",
		Type:      TypeSchedule,
		Name:      "Daily Report",
		Spec:      "0 9 * * *",
		Prompt:    "Generate daily report",
		Enabled:   true,
		CreatedAt: now,
	}

	if task.ID != "task-1" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Type != TypeSchedule {
		t.Errorf("Type = %q", task.Type)
	}
	if task.Name != "Daily Report" {
		t.Errorf("Name = %q", task.Name)
	}
	if task.Spec != "0 9 * * *" {
		t.Errorf("Spec = %q", task.Spec)
	}
	if !task.Enabled {
		t.Error("Enabled should be true")
	}
	if task.At != nil {
		t.Error("At should be nil for schedule type")
	}
}

func TestTaskAlarmType(t *testing.T) {
	at := time.Now().Add(1 * time.Hour)
	task := Task{
		ID:   "alarm-1",
		Type: TypeAlarm,
		Name: "One-time Reminder",
		At:   &at,
	}

	if task.Type != TypeAlarm {
		t.Errorf("Type = %q", task.Type)
	}
	if task.At == nil {
		t.Error("At should not be nil for alarm type")
	}
	if !task.At.Equal(at) {
		t.Error("At time mismatch")
	}
}

func TestBudgetZeroValue(t *testing.T) {
	var b Budget
	if b.MaxTurns != 0 {
		t.Errorf("MaxTurns zero = %d", b.MaxTurns)
	}
	if b.MaxTokens != 0 {
		t.Errorf("MaxTokens zero = %d", b.MaxTokens)
	}
	if b.MaxWall != 0 {
		t.Errorf("MaxWall zero = %v", b.MaxWall)
	}
	if b.MaxUSD != 0 {
		t.Errorf("MaxUSD zero = %f", b.MaxUSD)
	}
}
