package internal

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type ProtoEditor struct {
	xmlFile string
	docRoot *XmlElement
}

func FindJetBrainsRootAndOpen(findFrom string) (*ProtoEditor, error) {
	findFrom, err := filepath.Abs(findFrom)
	if err != nil {
		return nil, fmt.Errorf("func filepath.Abs() error: [%w]", err)
	}
	var ideaDir string
	for {
		ideaDir = filepath.Join(findFrom, ".idea")
		dirInfo, err := os.Stat(ideaDir)
		if err == nil && dirInfo.IsDir() {
			break
		}
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("func os.Stat() error: [%w]", err)
		}
		nextFindFrom := filepath.Dir(findFrom)
		if nextFindFrom == findFrom {
			return nil, errors.New("dir not found: [.idea]")
		}
		findFrom = nextFindFrom
	}
	protoEditor := &ProtoEditor{xmlFile: filepath.Join(ideaDir, "protoeditor.xml"), docRoot: &XmlElement{}}
	fileBytes, err := os.ReadFile(protoEditor.xmlFile)
	if err == nil || os.IsNotExist(err) {
		if err = xml.Unmarshal(fileBytes, protoEditor.docRoot); err != nil && err != io.EOF {
			return nil, fmt.Errorf("func xml.Unmarshal() error: [%w]", err)
		}
		return protoEditor, nil
	}
	return nil, fmt.Errorf("func os.ReadFile() error: [%w]", err)
}

func (thisP *ProtoEditor) ConfigProtoPath(goPkg string, protoPaths []string) {
	var pComponent *XmlElement
	if pComponent = findFirstEle(thisP.docRoot.Elements, func(e *XmlElement) bool {
		return e.XMLName.Local == "component" &&
			findFirstAttr(e.Attrs, func(a *xml.Attr) bool {
				return a.Name.Local == "name" && a.Value == "ProtobufLanguageSettings"
			}) != nil
	}); pComponent == nil {
		pComponent = &XmlElement{
			XMLName: xmlLocalName("component"),
			Attrs: []*xml.Attr{{
				Name:  xmlLocalName("name"),
				Value: "ProtobufLanguageSettings",
			}},
		}
		thisP.docRoot.Elements = append(thisP.docRoot.Elements, pComponent)
	}

	if pOptionAutoConfigEnabled := findFirstEle(pComponent.Elements, func(e *XmlElement) bool {
		return e.XMLName.Local == "option" &&
			findFirstAttr(e.Attrs, func(a *xml.Attr) bool {
				return a.Name.Local == "name" && a.Value == "autoConfigEnabled"
			}) != nil
	}); pOptionAutoConfigEnabled != nil {
		if pValue := findFirstAttr(pOptionAutoConfigEnabled.Attrs, func(a *xml.Attr) bool {
			return a.Name.Local == "value"
		}); pValue != nil {
			pValue.Value = "false"
		} else {
			pOptionAutoConfigEnabled.Attrs = append(pOptionAutoConfigEnabled.Attrs, &xml.Attr{
				Name:  xmlLocalName("value"),
				Value: "false",
			})
		}
	} else {
		pComponent.Elements = append(pComponent.Elements, &XmlElement{
			XMLName: xmlLocalName("option"),
			Attrs: []*xml.Attr{
				{
					Name:  xmlLocalName("name"),
					Value: "autoConfigEnabled",
				},
				{
					Name:  xmlLocalName("value"),
					Value: "false",
				},
			},
		})
	}

	if pOptionDescriptorPath := findFirstEle(pComponent.Elements, func(e *XmlElement) bool {
		return e.XMLName.Local == "option" &&
			findFirstAttr(e.Attrs, func(a *xml.Attr) bool {
				return a.Name.Local == "name" && a.Value == "descriptorPath"
			}) != nil
	}); pOptionDescriptorPath != nil {
		if pValue := findFirstAttr(pOptionDescriptorPath.Attrs, func(a *xml.Attr) bool {
			return a.Name.Local == "value"
		}); pValue != nil {
			pValue.Value = "google/protobuf/descriptor.proto"
		} else {
			pOptionDescriptorPath.Attrs = append(pOptionDescriptorPath.Attrs, &xml.Attr{
				Name:  xmlLocalName("value"),
				Value: "google/protobuf/descriptor.proto",
			})
		}
	} else {
		pComponent.Elements = append(pComponent.Elements, &XmlElement{
			XMLName: xmlLocalName("option"),
			Attrs: []*xml.Attr{
				{
					Name:  xmlLocalName("name"),
					Value: "descriptorPath",
				},
				{
					Name:  xmlLocalName("value"),
					Value: "google/protobuf/descriptor.proto",
				},
			},
		})
	}

	var pOptionImportPathEntries *XmlElement
	if pOptionImportPathEntries = findFirstEle(pComponent.Elements, func(e *XmlElement) bool {
		return e.XMLName.Local == "option" &&
			findFirstAttr(e.Attrs, func(a *xml.Attr) bool {
				return a.Name.Local == "name" && a.Value == "importPathEntries"
			}) != nil
	}); pOptionImportPathEntries == nil {
		pOptionImportPathEntries = &XmlElement{}
		pComponent.Elements = append(pComponent.Elements, pOptionImportPathEntries)
	}

	pOptionImportPathEntries.XMLName = xmlLocalName("option")
	pOptionImportPathEntries.Attrs = []*xml.Attr{{
		Name:  xmlLocalName("name"),
		Value: "importPathEntries",
	}}

	var pList *XmlElement
	if pList = findFirstEle(pOptionImportPathEntries.Elements, func(e *XmlElement) bool {
		return e.XMLName.Local == "list"
	}); pList == nil {
		pList = &XmlElement{
			XMLName: xmlLocalName("list"),
		}
		pOptionImportPathEntries.Elements = append(pOptionImportPathEntries.Elements, pList)
	}

	deleteCount := 0
	for i := len(pList.Elements) - 1; i >= 0; i-- {
		if pList.Elements[i].Comment == "" || pList.Elements[i].Comment == goPkg {
			pList.Elements[i] = pList.Elements[len(pList.Elements)-1-deleteCount]
			deleteCount++
		}
	}
	pList.Elements = pList.Elements[:len(pList.Elements)-deleteCount]

	for _, protoPath := range protoPaths {
		pList.Elements = append(pList.Elements, &XmlElement{
			XMLName: xmlLocalName("ImportPathEntry"),
			Elements: []*XmlElement{{
				XMLName: xmlLocalName("option"),
				Attrs: []*xml.Attr{
					{
						Name:  xmlLocalName("name"),
						Value: "location",
					},
					{
						Name:  xmlLocalName("value"),
						Value: "file://" + filepath.ToSlash(protoPath),
					},
				},
			}},
			Comment: goPkg,
		})
	}
}

func (thisP *ProtoEditor) Save() error {
	buf := bytes.Buffer{}
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>")
	encoder := xml.NewEncoder(&buf)
	if err := encoder.Encode(thisP.docRoot); err != nil {
		return fmt.Errorf("func encoder.Encode() error: [%w]", err)
	}
	if err := encoder.Flush(); err != nil {
		return fmt.Errorf("func encoder.Flush() error: [%w]", err)
	}
	if err := os.WriteFile(thisP.xmlFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("func os.WriteFile() error: [%w]", err)
	}
	return nil
}

type XmlElement struct {
	XMLName  xml.Name
	Attrs    []*xml.Attr   `xml:",any,attr"`
	Elements []*XmlElement `xml:",any"`
	Comment  string        `xml:",comment"`
}

func xmlLocalName(local string) xml.Name {
	return xml.Name{Local: local}
}

func findFirstEle(es []*XmlElement, test func(e *XmlElement) bool) *XmlElement {
	for _, e := range es {
		if test(e) {
			return e
		}
	}
	return nil
}

func findFirstAttr(as []*xml.Attr, test func(a *xml.Attr) bool) *xml.Attr {
	for _, a := range as {
		if test(a) {
			return a
		}
	}
	return nil
}
