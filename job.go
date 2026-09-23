package tango

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrDuplicateJob is returned by Registry.RegisterJob when a Job with the
// same Name has already been registered.
var ErrDuplicateJob = errors.New("tango: duplicate job name")

// Job is one named, interval-based unit of recurring work. RegisterJob records
// Jobs declaratively; execution is owned by ServeContext's scheduler.
type Job struct {
	Name     string
	Interval time.Duration
	Run      func(context.Context) error
}

// RegisterJob records job for execution when the application is served. It
// never invokes Run. Name must not be blank, Interval must be positive, Run
// must be non-nil, and Name must be unique within the Registry.
func (r *Registry) RegisterJob(job Job) error {
	if strings.TrimSpace(job.Name) == "" {
		return fmt.Errorf("tango: job name must not be blank")
	}
	if job.Interval <= 0 {
		return fmt.Errorf("tango: job %q interval must be positive", job.Name)
	}
	if job.Run == nil {
		return fmt.Errorf("tango: job %q must set Run", job.Name)
	}
	if _, exists := r.jobNames[job.Name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateJob, job.Name)
	}

	if r.jobNames == nil {
		r.jobNames = make(map[string]struct{})
	}
	r.jobNames[job.Name] = struct{}{}
	r.jobs = append(r.jobs, job)

	return nil
}
