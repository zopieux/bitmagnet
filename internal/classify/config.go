package classify

type BannedAction string

const (
	BannedActionKeep    BannedAction = "keep"
	BannedActionDelete  BannedAction = "delete"
	BannedActionArchive BannedAction = "archive"
)

type Config struct {
	BannedAction BannedAction `validate:"oneof=keep delete archive"`
}

func NewDefaultConfig() Config {
	return Config{
		BannedAction: BannedActionDelete,
	}
}
