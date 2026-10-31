package p2p

import "sync"

var networkNotice struct {
	sync.RWMutex
	fn func()
}

// SetNetworkFailureHandler connects failures from background guest connections
// (which have no frontend API call) to the same recovery UI as launcher actions.
func SetNetworkFailureHandler(fn func()) {
	networkNotice.Lock()
	networkNotice.fn = fn
	networkNotice.Unlock()
}

func reportNetworkFailure() {
	networkNotice.RLock()
	fn := networkNotice.fn
	networkNotice.RUnlock()
	if fn != nil {
		fn()
	}
}
