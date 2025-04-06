package help

import (
	_ "embed"
	"encoding/json"
)

//go:embed help.json
var helpContent []byte

type MinMax struct {
	Min float64
	Max float64
}

type Row struct {
	Image   string
	Layer   float64
	Channel string
}

type ChannelDescription struct {
	Description string
	Limits      *MinMax
}

type Column struct {
	Description string
	Red         *ChannelDescription
	Green       *ChannelDescription
	Blue        *ChannelDescription
	Alpha       *ChannelDescription
}

type LUT struct {
	Width   MinMax
	Height  MinMax
	Rows    []Row
	Columns []Column
}

type Help struct {
	MaterialLUT *LUT
	PatternLUT  *LUT
}

var helpStruct Help = Help{
	MaterialLUT: nil,
	PatternLUT:  nil,
}

func GetHelp() (Help, error) {
	if helpStruct.MaterialLUT != nil {
		return helpStruct, nil
	}
	err := json.Unmarshal(helpContent, &helpStruct)
	return helpStruct, err
}
