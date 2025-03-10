package template

import (
	"context"
	htmlTemplate "html/template"
	"io"
	"io/fs"
	"path/filepath"
	"sync"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/go-sonic/sonic/event"
	"github.com/go-sonic/sonic/util/xerr"
)

type Template struct {
	HtmlTemplate   *htmlTemplate.Template
	TextTemplate   *template.Template
	sharedVariable map[string]any
	lock           sync.RWMutex
	watcher        *fsnotify.Watcher
	logger         *zap.Logger
	paths          []string
	funcMap        map[string]any
	bus            event.Bus
}

func NewTemplate(logger *zap.Logger, bus event.Bus) *Template {
	t := &Template{
		sharedVariable: map[string]any{},
		logger:         logger,
		funcMap:        map[string]any{},
		bus:            bus,
	}
	t.addUtilFunc()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	t.watcher = watcher
	go t.Watch()
	return t
}

func (t *Template) Reload(paths []string) error {
	t.bus.Publish(context.Background(), &event.ThemeFileUpdatedEvent{})
	return nil
}

func (t *Template) Load(paths []string) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.paths = paths
	filenames := make([]string, 0)
	for _, templateDir := range paths {

		err := filepath.Walk(templateDir, func(path string, info fs.FileInfo, err error) error {
			if filepath.Ext(path) == ".tmpl" {
				filenames = append(filenames, path)
				if err := t.watcher.Add(path); err != nil {
					t.logger.Error("template.load.fsnotify.Add", zap.Error(err))
				}
			}
			return nil
		})
		if err != nil {
			return xerr.WithMsg(err, "traverse template dir err").WithStatus(xerr.StatusInternalServerError)
		}
	}

	ht, err := htmlTemplate.New("").Funcs(t.funcMap).ParseFiles(filenames...)
	if err != nil {
		return xerr.WithMsg(err, "parse template err").WithStatus(xerr.StatusInternalServerError)
	}

	tt, err := template.New("").Funcs(t.funcMap).ParseFiles(filenames...)
	if err != nil {
		return xerr.WithMsg(err, "parse template err").WithStatus(xerr.StatusInternalServerError)
	}
	t.TextTemplate = tt
	t.HtmlTemplate = ht
	return nil
}

func (t *Template) SetSharedVariable(name string, value interface{}) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.sharedVariable[name] = value
}

func (t *Template) Execute(wr io.Writer, data Model) error {
	dataMap, err := t.wrapData(data)
	if err != nil {
		return err
	}
	return t.HtmlTemplate.Execute(wr, dataMap)
}

func (t *Template) ExecuteTemplate(wr io.Writer, name string, data Model) error {
	dataMap, err := t.wrapData(data)
	if err != nil {
		return err
	}
	return t.HtmlTemplate.ExecuteTemplate(wr, name, dataMap)
}

func (t *Template) ExecuteText(wr io.Writer, data Model) error {
	dataMap, err := t.wrapData(data)
	if err != nil {
		return err
	}
	return t.TextTemplate.Execute(wr, dataMap)
}

func (t *Template) ExecuteTextTemplate(wr io.Writer, name string, data Model) error {
	dataMap, err := t.wrapData(data)
	if err != nil {
		return err
	}
	return t.TextTemplate.ExecuteTemplate(wr, name, dataMap)
}

func (t *Template) wrapData(data Model) (map[string]any, error) {
	if data == nil {
		return nil, nil
	}

	t.lock.RLock()
	defer t.lock.RUnlock()
	data.MergeAttributes(t.sharedVariable)
	data["now"] = time.Now()
	return data, nil
}

func (t *Template) AddFunc(name string, fn interface{}) {
	if t.HtmlTemplate != nil {
		panic("the template has been parsed")
	}
	t.funcMap[name] = fn
}

func (t *Template) addUtilFunc() {
	t.funcMap["unix_milli_time_format"] = func(format string, t int64) string {
		return time.UnixMilli(t).Format(format)
	}
	t.funcMap["noescape"] = func(str string) htmlTemplate.HTML {
		return htmlTemplate.HTML(str)
	}
	for name, f := range sprig.FuncMap() {
		t.funcMap[name] = f
	}
}
