package stage

import "context"

// inputUntilDone relays input until it closes or ctx ends, and then closes the
// channel it returns. A stage that ranges over the relay notices the end of its
// execution (the pipeline's idle timeout, say) at once, instead of on the next
// element, which in a live session may never come (#2823).
func inputUntilDone(ctx context.Context, input <-chan StreamElement) <-chan StreamElement {
	out := make(chan StreamElement)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case elem, ok := <-input:
				if !ok {
					return
				}
				select {
				case out <- elem:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}
