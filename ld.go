package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"text/template"
)

var ldArgs = []string{"-G 0", "-nostartfiles", "-nodefaultlibs", "-nostdinc", "-M"}

func createLdScript(w *Wave) (io.Reader, error) {
	t := `
ENTRY(_start)
MEMORY {
    ram (RX) : ORIGIN = 0x80000000, LENGTH = 0x7FFFFFFF
    ram.bss (RW) : ORIGIN = 0x80000000, LENGTH = 0x7FFFFFFF
}
SECTIONS {
    _RomStart = 0x1000;
    _RomSize = _RomStart;
    ..generatedStartEntry 0x80000400 : AT(_RomSize)
    {
      a.out (.text)
      a.out (.bss)
      a.out (.data)
    } > ram
    {{range .ObjectSegments -}}
      {{if (gt .Positioning.Address 0x80000400)}}
        _RomSize = ({{.Positioning.Address}} - 0x80000400) + _RomStart;
      {{end}}
    _{{.Name}}SegmentRomStart = _RomSize;
    ..{{.Name}}
    {{if ne .Positioning.AfterSegment ""}}
        ADDR(..{{.Positioning.AfterSegment}}.bss) + SIZEOF(..{{.Positioning.AfterSegment}}.bss)
    {{else if ne (index .Positioning.AfterMinSegment 0) ""}}
        MIN(
          ADDR(..{{index .Positioning.AfterMinSegment 0}}.bss) + SIZEOF(..{{index .Positioning.AfterMinSegment 0}}.bss),
          ADDR(..{{index .Positioning.AfterMinSegment 1}}.bss) + SIZEOF(..{{index .Positioning.AfterMinSegment 1}}.bss))
    {{else if ne (index .Positioning.AfterMaxSegment 0) ""}}
        MAX(
          ADDR(..{{index .Positioning.AfterMaxSegment 0}}.bss) + SIZEOF(..{{index .Positioning.AfterMaxSegment 0}}.bss),
          ADDR(..{{index .Positioning.AfterMaxSegment 1}}.bss) + SIZEOF(..{{index .Positioning.AfterMaxSegment 1}}.bss))
    {{else if not (eq .Positioning.Address 0)}}
      {{.Positioning.Address}}
    {{end}}
    : AT(_RomSize)
    {
      _{{.Name}}SegmentStart = .;
      . = ALIGN(0x10);
      _{{.Name}}SegmentTextStart = .;
      {{range .Includes -}}
        {{.}} (.text)
      {{end}}
      _{{.Name}}SegmentTextEnd = .;
      _{{.Name}}SegmentDataStart = .;
      {{range .Includes -}}
        {{.}} (.data)
      {{end}}
      {{range .Includes -}}
        {{.}} (.rodata*)
      {{end}}
      {{range .Includes -}}
        {{.}} (.sdata)
      {{end}}
      . = ALIGN(0x10);
      _{{.Name}}SegmentDataEnd = .;
    } {{if (gt .Positioning.Address 0x80000400)}} > ram {{end}}
    _RomSize += (_{{.Name}}SegmentDataEnd - _{{.Name}}SegmentTextStart);
    _{{.Name}}SegmentRomEnd = _RomSize;

    ..{{.Name}}.bss ADDR(..{{.Name}}) + SIZEOF(..{{.Name}}) (NOLOAD) :
    {
      . = ALIGN(0x10);
      _{{.Name}}SegmentBssStart = .;
      {{range .Includes -}}
        {{.}} (.sbss)
      {{end}}
      {{range .Includes -}}
        {{.}} (.scommon)
      {{end}}
      {{range .Includes -}}
        {{.}} (.bss)
      {{end}}
      {{range .Includes -}}
        {{.}} (COMMON)
      {{end}}
      . = ALIGN(0x10);
      _{{.Name}}SegmentBssEnd = .;
      _{{.Name}}SegmentEnd = .;
    } {{if (gt .Positioning.Address 0x80000400)}} > ram.bss {{end}}
    _{{.Name}}SegmentBssSize =  _{{.Name}}SegmentBssEnd - _{{.Name}}SegmentBssStart;
  {{ end }}
  {{range .RawSegments -}}
    _{{.Name}}SegmentRomStart = _RomSize;
    ..{{.Name}} : AT(_RomSize)
    {
      . = ALIGN(0x10);
      _{{.Name}}SegmentDataStart = .;
      {{range .Includes -}}
      "{{.}}.o"
      {{end}}
      . = ALIGN(0x10);
      _{{.Name}}SegmentDataEnd = .;
    } > ram
    _RomSize += SIZEOF(..{{.Name}});
    _{{.Name}}SegmentRomEnd = _RomSize;
  {{ end }}
  /DISCARD/ :
  {
    /* Discard everything we haven't explicitly used. */
    *(.eh_frame)
    *(.MIPS.abiflags)
  }
  _RomEnd = _RomSize;
}
`
	tmpl, err := template.New("test").Parse(t)
	if err != nil {
		return nil, err
	}
	b := &bytes.Buffer{}
	err = tmpl.Execute(b, w)
	if err == nil {
		log.Debugln("Ld script generated:\n", b.String())
	}
	return b, err
}

func LinkSpec(w *Wave, ld Runner, entry io.Reader) (io.Reader, error) {
	name := w.Name
	log.Infof("Linking spec \"%s\".", name)
	ldscript, err := createLdScript(w)
	if err != nil {
		return nil, err
	}
	outputPath := fmt.Sprintf("%s.out", name)
	mappedInputs := map[string]io.Reader{
		"ld-script": ldscript,
	}
	return NewMappedFileRunner(ld, mappedInputs, outputPath).Run( /* stdin=*/ nil, append(ldArgs, "-dT", "ld-script", "-o", outputPath))
}
func TempFileName(suffix string) string {
	randBytes := make([]byte, 16)
	rand.Read(randBytes)
	return filepath.Join(os.TempDir(), hex.EncodeToString(randBytes)+suffix)
}

func BinarizeObject(obj io.Reader, objcopy Runner) (io.Reader, error) {
	outputBin := TempFileName(".bin")
	mappedInputs := map[string]io.Reader{
		"objFile": obj,
	}
	return NewMappedFileRunner(objcopy, mappedInputs, outputBin).Run( /* stdin=*/ nil, []string{"-O", "binary", "objFile", outputBin})
}

func CreateRawObjectWrapper(r io.Reader, outputName string, ld Runner) (io.Reader, error) {
	mappedInputs := map[string]io.Reader{
		"input": r,
	}
	return NewMappedFileRunner(ld, mappedInputs, outputName).Run( /* stdin=*/ nil, []string{"-r", "-b", "binary", "-o", outputName, "input"})
}
