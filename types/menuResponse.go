package types

type MenuResponse uint8

const (
	MenuResponseNone             MenuResponse = 0
	MenuResponseImageOpen        MenuResponse = 1
	MenuResponseImageSave        MenuResponse = 2
	MenuResponseImageSaveAs      MenuResponse = 3
	MenuResponseImageNew         MenuResponse = 4
	MenuResponseViewChannels     MenuResponse = 5
	MenuResponseViewColor        MenuResponse = 6
	MenuResponseViewHelp         MenuResponse = 7
	MenuResponseViewGrid         MenuResponse = 8
	MenuResponseUndo             MenuResponse = 9
	MenuResponseRedo             MenuResponse = 10
	MenuResponseBulkConvertToDDS MenuResponse = 11
	MenuResponseBulkConvertToEXR MenuResponse = 12
)
