package classify

type BannedAction string

const (
	BannedActionKeep    BannedAction = "keep"
	BannedActionDelete  BannedAction = "delete"
	BannedActionArchive BannedAction = "archive"
)

type ScriptAction string

const (
	ScriptActionKeep    ScriptAction = "keep"
	ScriptActionDelete  ScriptAction = "delete"
	ScriptActionArchive ScriptAction = "archive"
)

type Config struct {
	BannedAction   BannedAction `validate:"oneof=keep delete archive"`
	CyrillicAction ScriptAction `validate:"oneof=keep delete archive"`
	KoreanAction   ScriptAction `validate:"oneof=keep delete archive"`
	ChineseAction  ScriptAction `validate:"oneof=keep delete archive"`
	JapaneseAction ScriptAction `validate:"oneof=keep delete archive"`
	ThaiAction     ScriptAction `validate:"oneof=keep delete archive"`
}

func NewDefaultConfig() Config {
	return Config{
		BannedAction:   BannedActionDelete,
		CyrillicAction: ScriptActionKeep,
		KoreanAction:   ScriptActionKeep,
		ChineseAction:  ScriptActionKeep,
		JapaneseAction: ScriptActionKeep,
		ThaiAction:     ScriptActionKeep,
	}
}
