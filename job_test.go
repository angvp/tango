package tango

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRegisterJobValidation(t *testing.T) {
	run := func(context.Context) error { return nil }
	tests := []struct {
		name string
		job  Job
	}{
		{
			name: "empty name",
			job:  Job{Interval: time.Second, Run: run},
		},
		{
			name: "whitespace-only name",
			job:  Job{Name: " \t\n", Interval: time.Second, Run: run},
		},
		{
			name: "zero interval",
			job:  Job{Name: "zero", Run: run},
		},
		{
			name: "negative interval",
			job:  Job{Name: "negative", Interval: -time.Second, Run: run},
		},
		{
			name: "nil run",
			job:  Job{Name: "nil-run", Interval: time.Second},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			if err := r.RegisterJob(tt.job); err == nil {
				t.Fatal("RegisterJob() error = nil, want validation error")
			}
			if len(r.jobs) != 0 {
				t.Fatalf("rejected registration recorded %d jobs, want 0", len(r.jobs))
			}
		})
	}
}

func TestRegisterJobRejectsDuplicateName(t *testing.T) {
	r := NewRegistry()
	first := Job{
		Name:     "cleanup",
		Interval: time.Minute,
		Run:      func(context.Context) error { return nil },
	}
	if err := r.RegisterJob(first); err != nil {
		t.Fatalf("first RegisterJob(): %v", err)
	}

	err := r.RegisterJob(Job{
		Name:     "cleanup",
		Interval: time.Hour,
		Run:      func(context.Context) error { return nil },
	})
	if !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("RegisterJob() error = %v, want errors.Is ErrDuplicateJob", err)
	}
	if len(r.jobs) != 1 {
		t.Fatalf("registered jobs = %d, want 1 after duplicate rejection", len(r.jobs))
	}
}

func TestRegisterJobRecordsInOrderWithoutRunning(t *testing.T) {
	r := NewRegistry()
	runCalled := false
	want := []string{"first", "second", "third"}

	for _, name := range want {
		job := Job{
			Name:     name,
			Interval: time.Second,
			Run: func(context.Context) error {
				runCalled = true
				return nil
			},
		}
		if err := r.RegisterJob(job); err != nil {
			t.Fatalf("RegisterJob(%q): %v", name, err)
		}
	}

	if runCalled {
		t.Fatal("RegisterJob invoked Job.Run")
	}
	if len(r.jobs) != len(want) {
		t.Fatalf("registered jobs = %d, want %d", len(r.jobs), len(want))
	}
	for i, name := range want {
		if r.jobs[i].Name != name {
			t.Fatalf("jobs[%d].Name = %q, want %q", i, r.jobs[i].Name, name)
		}
	}
}

func TestRegisterJobPreservesInstalledAppRegistrationOrder(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"app-a", "app-b", "app-c"} {
		name := name
		if err := r.Register(NewApp(name, func(registry *Registry) error {
			return registry.RegisterJob(Job{
				Name:     "job-" + name,
				Interval: time.Second,
				Run:      func(context.Context) error { return nil },
			})
		})); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	if err := r.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration(): %v", err)
	}
	want := []string{"job-app-a", "job-app-b", "job-app-c"}
	if len(r.jobs) != len(want) {
		t.Fatalf("registered jobs = %d, want %d", len(r.jobs), len(want))
	}
	for i, name := range want {
		if r.jobs[i].Name != name {
			t.Fatalf("jobs[%d].Name = %q, want %q", i, r.jobs[i].Name, name)
		}
	}
}
