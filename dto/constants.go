package dto

import "time"

// ChannelTimeout is the timeout for channel operations to prevent deadlock.
// Used by both TUI and Docker layers for consistent behavior.
const ChannelTimeout = 5 * time.Second
