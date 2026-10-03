package accesslog

import (
	"time"

	"github.com/pravbeseda/monitor/internal/weblog"
)

// WaitReadBack returns once every reading back the sensor started has ended.
func (s *Sensor) WaitReadBack() { s.backs.Wait() }

// HoldReadBack keeps every reading back started from now on running until release is
// called.
func (s *Sensor) HoldReadBack() (release func()) {
	held := make(chan struct{})
	read := s.readBack
	s.readBack = func(b *weblog.Back, since time.Time, each func(weblog.Request)) (time.Time, error) {
		<-held
		return read(b, since, each)
	}
	return func() {
		close(held)
		s.backs.Wait()
	}
}
