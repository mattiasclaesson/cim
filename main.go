package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/roffe/cim/pkg/cim"
)

func init() {
	//gin.SetMode(gin.ReleaseMode)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	/*
		if len(os.Args) < 2 {
			log.Fatal("missing input filename")
		}
	*/
}

func main() {
	/*
		filename := os.Args[1]
		fw, err := cim.Load(filename)
		if err != nil {
			log.Fatal(err)
		}
		if err := fw.Validate(); err != nil {
			log.Fatal(err)
		}
	*/
	//fw.Pretty()
	//fw.Dump()
	//log.Println(fw.Vin.Value)
	fmt.Println("open http://localhost:8080")
	web()

}

var templateHelpers = template.FuncMap{
	"printHex": func(v interface{}) template.HTML {
		return template.HTML(fmt.Sprintf("%X", v))
	},
	"print": func(v interface{}) template.HTML {
		return template.HTML(fmt.Sprintf("%s", v))
	},
	"isoDate": func(t time.Time) template.HTML {
		return template.HTML(t.Format(cim.IsoDate))
	},
	"boolChecked": func(b bool) template.HTML {
		if b {
			return template.HTML("checked")
		}
		return template.HTML("")
	},
}

func web() {
	r := gin.Default()
	r.MaxMultipartMemory = 1 << 20
	//r.LoadHTMLGlob("templates/*.tmpl")

	if tmpl, err := template.New("projectViews").Funcs(templateHelpers).ParseGlob("templates/*.tmpl"); err == nil {
		r.SetHTMLTemplate(tmpl)
	} else {
		panic(err)
	}

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "upload.tmpl", nil)
	})

	r.POST("/save", func(c *gin.Context) {
		file := c.PostForm("file")
		filename := c.PostForm("filename")

		b, err := base64.StdEncoding.DecodeString(file)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		for i, bb := range b {
			b[i] = bb ^ 0xff
		}

		fw, err := cim.LoadBytes(filename, b)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		bs, err := fw.Bytes()
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		for i, b := range bs {
			bs[i] = b ^ 0xFF
		}

		contentLength := int64(len(bs))
		contentType := "application/octet-stream"

		extraHeaders := map[string]string{
			"Content-Disposition": `attachment; filename="` + filepath.Base(filename) + `"`,
		}
		r := bytes.NewReader(bs)
		c.DataFromReader(http.StatusOK, contentLength, contentType, r, extraHeaders)
	})

	r.POST("/", func(c *gin.Context) {
		buf, filename, n, err := getFileFromCtx(c)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		if n < 512 || n > 512 {
			c.String(http.StatusInternalServerError, "invalid bin size")
			return
		}

		if buf[0] == 0x20 {
			for i, b := range buf {
				buf[i] = b ^ 0xff
			}
		}

		fw, err := cim.LoadBytes(filename, buf)
		if err != nil {
			c.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		if err := fw.Validate(); err != nil {
			c.Error(err)
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		bs, err := fw.Bytes()
		if err != nil {
			c.Error(err)
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		hexRow := strings.Builder{}
		asciiColumns := strings.Builder{}

		pos := 0
		offset := 0
		width := 40
		for _, bb := range bs {
			if pos == 0 {
				hexRow.WriteString(`<div class="hexRow">` + "\n" +
					"\t" + `<div class="addrColumn"><b>` + fmt.Sprintf("%03X", offset) + "</b></div>\n" +
					"\t" + `<div class="hexColumns">` + "\n")
			}

			hexRow.WriteString(fmt.Sprintf("\t\t"+`<div class="hexByte byte-%d" data-i="%d">%02X</div>`+"\n", offset, offset, bb))
			asciiColumns.WriteString(fmt.Sprintf(`<div class="asciiByte byte-%d" data-i="%d">%s</div>`+"\n", offset, offset, ps(bb)))
			if pos == width {
				hexRow.WriteString("</div>\n")
				hexRow.WriteString(`<div class="asciiColumns">` + "\n")
				hexRow.WriteString(asciiColumns.String())
				hexRow.WriteString("</div>\n")
				hexRow.WriteString("</div>\n")
				asciiColumns.Reset()
				pos = 0
				offset++
				continue
			}
			pos++
			offset++
		}
		if pos <= width {
			for i := pos; i <= width; i++ {
				hexRow.WriteString(`<div class="fillByte">&nbsp;&nbsp;</div>` + "\n")
			}
			hexRow.WriteString("</div>")
			hexRow.WriteString(`<div class="asciiColumns">` + "\n")
			hexRow.WriteString(asciiColumns.String())
			hexRow.WriteString("</div>\n")
			hexRow.WriteString("</div>\n")
			asciiColumns.Reset()
		}

		hexRow.WriteString("</div>")

		b64 := base64.StdEncoding.EncodeToString(bs)

		c.HTML(http.StatusOK, "hex.tmpl", gin.H{
			"filename": filepath.Base(filename),
			"fw":       fw,
			"B64":      b64,
			"Hexview":  template.HTML(hexRow.String()),
		})
	})

	if err := r.Run(); err != nil {
		log.Fatal(err)
	} // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}

func getFileFromCtx(c *gin.Context) ([]byte, string, int64, error) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		return nil, "", 0, fmt.Errorf("getFileFromCtx err 1: %s", err.Error())
	}
	defer file.Close()

	buf := bytes.NewBuffer(nil)
	n, err := io.Copy(buf, file)
	if err != nil {
		return nil, "", 0, fmt.Errorf("getFileFromCtx err 2: %s", err.Error())
	}
	return buf.Bytes(), header.Filename, n, nil
}

func ps(b byte) string {
	a := uint8(b)
	if a == 0x00 {
		return "·"
	}
	if a == 0x20 {
		return "&nbsp"
	}
	if a == 0xFF {
		return "Ʃ"
	}
	if a <= 0x20 {
		return "˟"
	}

	if a >= 0x7F {
		return "˟"
	}

	if a == 0x3c || a == 0x3e {
		return "˟"
	}

	return fmt.Sprintf("%s", []byte{b})
}
