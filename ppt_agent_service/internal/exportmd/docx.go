package exportmd

import (
	"archive/zip"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

type wordDocument struct {
	XMLName xml.Name `xml:"w:document"`
	W       string   `xml:"xmlns:w,attr"`
	Body    wordBody `xml:"w:body"`
}

type wordBody struct {
	Paragraphs []wordParagraph `xml:"w:p"`
	SectPr     wordSectPr      `xml:"w:sectPr"`
}

type wordParagraph struct {
	Run wordRun `xml:"w:r"`
}

type wordRun struct {
	Text wordText `xml:"w:t"`
}

type wordText struct {
	Space string `xml:"xml:space,attr,omitempty"`
	Value string `xml:",chardata"`
}

type wordSectPr struct {
	PgSz   wordPgSz   `xml:"w:pgSz"`
	PgMar  wordPgMar  `xml:"w:pgMar"`
	Cols   wordCols   `xml:"w:cols"`
	DocGrid wordDocGrid `xml:"w:docGrid"`
}

type wordPgSz struct {
	W string `xml:"w:w,attr"`
	H string `xml:"w:h,attr"`
}
type wordPgMar struct {
	Top    string `xml:"w:top,attr"`
	Right  string `xml:"w:right,attr"`
	Bottom string `xml:"w:bottom,attr"`
	Left   string `xml:"w:left,attr"`
	Header string `xml:"w:header,attr"`
	Footer string `xml:"w:footer,attr"`
	Gutter string `xml:"w:gutter,attr"`
}
type wordCols struct {
	Space string `xml:"w:space,attr"`
}
type wordDocGrid struct {
	LinePitch string `xml:"w:linePitch,attr"`
}

func WriteLessonPlanDOCX(path string, markdown string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	paras := make([]wordParagraph, 0, len(lines))
	for _, ln := range lines {
		txt := strings.TrimRight(ln, " ")
		if txt == "" {
			paras = append(paras, wordParagraph{Run: wordRun{Text: wordText{Space: "preserve", Value: " "}}})
			continue
		}
		paras = append(paras, wordParagraph{Run: wordRun{Text: wordText{Space: "preserve", Value: txt}}})
	}
	doc := wordDocument{
		W: "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
		Body: wordBody{
			Paragraphs: paras,
			SectPr: wordSectPr{
				PgSz:    wordPgSz{W: "11906", H: "16838"},
				PgMar:   wordPgMar{Top: "1440", Right: "1440", Bottom: "1440", Left: "1440", Header: "708", Footer: "708", Gutter: "0"},
				Cols:    wordCols{Space: "708"},
				DocGrid: wordDocGrid{LinePitch: "360"},
			},
		},
	}
	docXML, err := xml.Marshal(doc)
	if err != nil {
		return err
	}
	write := func(name, content string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(content))
		return err
	}
	if err := write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`); err != nil {
		return err
	}
	if err := write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`); err != nil {
		return err
	}
	if err := write("word/document.xml", xml.Header+string(docXML)); err != nil {
		return err
	}
	return nil
}

