package types

type MenuResponse uint8

const (
	MenuResponseNone             MenuResponse = iota
	MenuResponseImageOpen        MenuResponse = iota
	MenuResponseImageSave        MenuResponse = iota
	MenuResponseImageSaveAs      MenuResponse = iota
	MenuResponseImageNew         MenuResponse = iota
	MenuResponseViewChannels     MenuResponse = iota
	MenuResponseViewColor        MenuResponse = iota
	MenuResponseViewHelp         MenuResponse = iota
	MenuResponseViewTools        MenuResponse = iota
	MenuResponseViewGrid         MenuResponse = iota
	MenuResponseUndo             MenuResponse = iota
	MenuResponseRedo             MenuResponse = iota
	MenuResponseCopy             MenuResponse = iota
	MenuResponseCut              MenuResponse = iota
	MenuResponsePaste            MenuResponse = iota
	MenuResponseBulkConvertToDDS MenuResponse = iota
	MenuResponseBulkConvertToEXR MenuResponse = iota
)
