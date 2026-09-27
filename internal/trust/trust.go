// Package trust integrates PortFlow's local CA with the operating system's
// trust store. Install/Uninstall are the only functions that ever modify
// trust settings, and they only run when a user explicitly invokes them.
package trust

import "errors"

// ErrUnsupported is returned on platforms with no adapter yet.
var ErrUnsupported = errors.New("trust store install is not supported on this platform yet — install the CA manually from ~/.portflow/ca/portflow-ca.pem")
