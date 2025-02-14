package help

import (
	_ "embed"
	"encoding/json"
	"fmt"
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

type PrimaryLUTStruct struct {
	Width   MinMax
	Height  MinMax
	Rows    []Row
	Columns []Column
}

type Help struct {
	PrimaryLUT *PrimaryLUTStruct
}

var helpStruct Help = Help{
	PrimaryLUT: nil,
}

func GetHelp() (Help, error) {
	if helpStruct.PrimaryLUT != nil {
		fmt.Printf("Help: PrimaryLUT = %v\n", helpStruct.PrimaryLUT)
		return helpStruct, nil
	}
	err := json.Unmarshal(helpContent, &helpStruct)
	fmt.Printf("Help: err = %v\n", err)
	if err == nil {
		fmt.Printf("Help: PrimaryLUT = %v\n", helpStruct.PrimaryLUT)
	}
	return helpStruct, err
}
