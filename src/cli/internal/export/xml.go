package export

import (
	"encoding/xml"
	"fmt"
	"io"
)

// writeXML holds the column name in an attribute rather than in the element
// name. A column can be called count(*) or 2024, and neither is a name XML
// allows an element to have; an attribute takes any of them.
type writeXML struct {
	raw     io.Writer
	out     *xml.Encoder
	columns []string
}

func newXML(out io.Writer) Writer {
	encoder := xml.NewEncoder(out)
	encoder.Indent("", "  ")
	return &writeXML{raw: out, out: encoder}
}

func (w *writeXML) Head(columns []string) error {
	w.columns = columns
	if _, err := io.WriteString(w.raw, xml.Header); err != nil {
		return fmt.Errorf("write the file: %w", err)
	}
	if err := w.out.EncodeToken(xml.StartElement{Name: xml.Name{Local: "result"}}); err != nil {
		return fmt.Errorf("write the file: %w", err)
	}
	return nil
}

func (w *writeXML) Row(values []any) error {
	row := xml.StartElement{Name: xml.Name{Local: "row"}}
	if err := w.out.EncodeToken(row); err != nil {
		return fmt.Errorf("write a row: %w", err)
	}
	for i, name := range w.columns {
		var value any
		if i < len(values) {
			value = values[i]
		}
		if err := w.field(name, value); err != nil {
			return err
		}
	}
	if err := w.out.EncodeToken(row.End()); err != nil {
		return fmt.Errorf("write a row: %w", err)
	}
	return nil
}

// field writes one column. A null is an empty element that says it is null,
// because an empty element and a column holding an empty string are otherwise
// the same thing on the page.
func (w *writeXML) field(name string, value any) error {
	field := xml.StartElement{
		Name: xml.Name{Local: "field"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "name"}, Value: name}},
	}
	if value == nil {
		field.Attr = append(field.Attr, xml.Attr{Name: xml.Name{Local: "null"}, Value: "true"})
		if err := w.out.EncodeToken(field); err != nil {
			return fmt.Errorf("write a value: %w", err)
		}
		if err := w.out.EncodeToken(field.End()); err != nil {
			return fmt.Errorf("write a value: %w", err)
		}
		return nil
	}
	if err := w.out.EncodeElement(Text(value), field); err != nil {
		return fmt.Errorf("write a value: %w", err)
	}
	return nil
}

func (w *writeXML) Close() error {
	if err := w.out.EncodeToken(xml.EndElement{Name: xml.Name{Local: "result"}}); err != nil {
		return fmt.Errorf("finish the file: %w", err)
	}
	if err := w.out.Flush(); err != nil {
		return fmt.Errorf("finish the file: %w", err)
	}
	return nil
}
