package failover

import "context"

// Pipeline can be used to construct a step-by-step process with deferred actions. By
// handling the errors and control-flow, it can provide an expressive mechanism for
// specifying pipelines.
func Pipeline(steps ...*pipelineStep) func(context.Context, context.Context) error {
	return func(ctx context.Context, deferCtx context.Context) error {
		for _, step := range steps {
			// Defer first, ensuring we always attempt our defer steps, even if the primary
			// action fails.
			for _, deferAction := range step.deferred {
				defer deferAction(deferCtx)
			}

			if err := step.action(ctx); err != nil {
				return err
			}
		}

		return nil
	}
}

type pipelineStep struct {
	action   func(context.Context) error
	deferred []func(context.Context) error
}

func Step(action func(context.Context) error) *pipelineStep {
	return &pipelineStep{action: action, deferred: []func(context.Context) error{}}
}

func (s *pipelineStep) Defer(deferred ...func(context.Context) error) *pipelineStep {
	s.deferred = deferred
	return s
}
