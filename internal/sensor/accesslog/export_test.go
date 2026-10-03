package accesslog

import (
	"context"
	"time"

	"github.com/pravbeseda/monitor/internal/weblog"
)

// WaitReadBack returns once every reading back the sensor started has ended.
func (s *Sensor) WaitReadBack() { s.backs.Wait() }

// HoldReadBack keeps every reading back started from now on waiting until release is
// called, and counts the requests each then hands over.
func (s *Sensor) HoldReadBack() (release func(), handed func() int) {
	held := make(chan struct{})
	count := make(chan int, 64)
	read := s.readBack
	s.readBack = func(ctx context.Context, b *weblog.Back, since time.Time, each func(weblog.Request)) (time.Time, error) {
		<-held
		n := 0
		reached, err := read(ctx, b, since, func(r weblog.Request) {
			n++
			each(r)
		})
		count <- n
		return reached, err
	}
	total := 0
	return func() {
			close(held)
			s.backs.Wait()
		}, func() int {
			for {
				select {
				case n := <-count:
					total += n
				default:
					return total
				}
			}
		}
}
