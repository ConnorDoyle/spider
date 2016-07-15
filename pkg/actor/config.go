package actor

// SystemConfig holds configuration parameters for an actor system.
type SystemConfig struct{}

// Config holds configuration parameters for an actor.
type Config struct {
	MailboxSize int64
}

// DefaultConfig returns a default actor configuration.
func DefaultConfig() Config {
	return Config{256}
}
