package clipboard

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abenz1267/walker/internal/config"
	"github.com/abenz1267/walker/internal/util"
)

const ClipboardName = "clipboard"

type Clipboard struct {
	general  config.GeneralModule
	items    []ClipboardItem
	entries  []util.Entry
	file     string
	imgTypes map[string]string
	max      int
}

type ClipboardItem struct {
	Content string    `json:"content,omitempty"`
	Time    time.Time `json:"time,omitempty"`
	Hash    string    `json:"hash,omitempty"`
	IsImg   bool      `json:"is_img,omitempty"`
}

func (c *Clipboard) General() *config.GeneralModule {
	return &c.general
}

func (c *Clipboard) Refresh() {
	c.general.IsSetup = !c.general.Refresh
}

func (c Clipboard) Cleanup() {}

func (c Clipboard) Entries(ctx context.Context, term string) []util.Entry {
	return c.entries
}

func (c *Clipboard) Setup(cfg *config.Config) bool {
	pth, _ := exec.LookPath("wl-copy")
	if pth == "" {
		log.Println("Clipboard disabled: currently wl-clipboard only.")
		return false
	}

	c.general = cfg.Builtins.Clipboard.GeneralModule

	c.file = filepath.Join(util.CacheDir(), "clipboard.gob")
	c.max = cfg.Builtins.Clipboard.MaxEntries

	c.imgTypes = make(map[string]string)
	c.imgTypes["image/png"] = "png"
	c.imgTypes["image/jpg"] = "jpg"
	c.imgTypes["image/jpeg"] = "jpeg"

	return true
}

func (c *Clipboard) SetupData(cfg *config.Config, ctx context.Context) {
	current := []ClipboardItem{}
	util.FromGob(c.file, &current)

	go c.watch()

	c.items = clean(current, c.file)

	for _, v := range c.items {
		c.entries = append(c.entries, itemToEntry(v))
	}

	c.general.IsSetup = true
	c.general.HasInitialSetup = true
}

func clean(entries []ClipboardItem, file string) []ClipboardItem {
	cleaned := []ClipboardItem{}

	for _, v := range entries {
		if !v.IsImg {
			cleaned = append(cleaned, v)
			continue
		}

		if _, err := os.Stat(v.Content); err == nil {
			cleaned = append(cleaned, v)
		}
	}

	util.ToGob(&cleaned, file)

	return cleaned
}

func (c Clipboard) exists(hash string) bool {
	for _, v := range c.items {
		if v.Hash == hash {
			return true
		}
	}

	return false
}

func getType() string {
	cmd := exec.Command("wl-paste", "--list-types")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(string(out))
		log.Panic(err)
	}

	fields := strings.Fields(string(out))

	return fields[0]
}

func getContent(primary ...bool) string {
	isPrimary := false
	if len(primary) > 0 {
		isPrimary = primary[0]
	}

	var cmd *exec.Cmd
	if isPrimary {
		cmd = exec.Command("wl-paste", "-p")
	} else {
		cmd = exec.Command("wl-paste")
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	txt := strings.TrimSpace(string(out))
	return txt
}

func calcHash(txt string) string {
	hash := md5.Sum([]byte(txt))
	strg := hex.EncodeToString(hash[:])
	return strg
}

func saveTmpImg(ext string) string {
	cmd := exec.Command("wl-paste")

	file := filepath.Join(util.TmpDir(), fmt.Sprintf("%d.%s", time.Now().Unix(), ext))

	outfile, err := os.Create(file)
	if err != nil {
		panic(err)
	}
	defer outfile.Close()

	cmd.Stdout = outfile

	err = cmd.Start()
	if err != nil {
		panic(err)
	}

	cmd.Wait()

	return file
}

func (c *Clipboard) watch() {
	for {
		time.Sleep(500 * time.Millisecond)

		contentList := []string{getContent(), getContent(true)}
		for _, content := range contentList {
			hash := calcHash(content)

			if c.exists(hash) {
				continue
			}

			if len(content) < 2 {
				continue
			}

			mimetype := getType()

			e := ClipboardItem{
				Content: content,
				Time:    time.Now(),
				Hash:    hash,
				IsImg:   false,
			}

			if val, ok := c.imgTypes[mimetype]; ok {
				file := saveTmpImg(val)
				e.Content = file
				e.IsImg = true
			} else {
				cmd := exec.Command("wl-copy")
				cmd.Stdin = strings.NewReader(e.Content)
				cmd.Start()
			}

			c.entries = append([]util.Entry{itemToEntry(e)}, c.entries...)
			c.items = append([]ClipboardItem{e}, c.items...)

			if len(c.items) >= c.max {
				c.items = slices.Clone(c.items[:c.max])
			}

			if len(c.entries) >= c.max {
				c.entries = slices.Clone(c.entries[:c.max])
			}

			util.ToGob(&c.items, c.file)
		}
	}
}

func itemToEntry(item ClipboardItem) util.Entry {
	entry := util.Entry{
		Label:            item.Content,
		Sub:              "Text",
		Exec:             "wl-copy",
		Piped:            util.Piped{Content: item.Content, Type: "string"},
		Categories:       []string{"clipboard"},
		Class:            "clipboard",
		Matching:         util.Fuzzy,
		LastUsed:         item.Time,
		RecalculateScore: true,
	}

	if item.IsImg {
		entry.Label = "Image"
		entry.Image = item.Content
		entry.Exec = "wl-copy"
		entry.Piped = util.Piped{
			Content: item.Content,
			Type:    "file",
		}
		entry.HideText = true
	}

	return entry
}
