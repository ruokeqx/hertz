/*
 * Copyright 2022 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * The MIT License (MIT)
 *
 * Copyright (c) 2014 Manuel Martínez-Almeida
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 * THE SOFTWARE.

 * This file may have been modified by CloudWeGo authors. All CloudWeGo
 * Modifications are Copyright 2022 CloudWeGo Authors.
 */

package render

import (
	"html/template"
	"log"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/fsnotify/fsnotify"
)

// Delims represents a set of Left and Right delimiters for HTML template rendering.
type Delims struct {
	// Left delimiter, defaults to {{.
	Left string
	// Right delimiter, defaults to }}.
	Right string
}

// HTMLRender interface is to be implemented by HTMLProduction and HTMLDebug.
type HTMLRender interface {
	// Instance returns an HTML instance.
	Instance(string, interface{}) Render
	Close() error
}

// HTMLProduction contains template reference and its delims.
type HTMLProduction struct {
	Template *template.Template
}

// HTML contains template reference and its name with given interface object.
type HTML struct {
	Template *template.Template
	Name     string
	Data     interface{}
}

var htmlContentType = "text/html; charset=utf-8"

// Instance (HTMLProduction) returns an HTML instance which it realizes Render interface.
func (r HTMLProduction) Instance(name string, data interface{}) Render {
	return HTML{
		Template: r.Template,
		Name:     name,
		Data:     data,
	}
}

func (r HTMLProduction) Close() error {
	return nil
}

// Render (HTML) executes template and writes its result with custom ContentType for response.
func (r HTML) Render(resp *protocol.Response) error {
	r.WriteContentType(resp)

	if r.Name == "" {
		return r.Template.Execute(resp.BodyWriter(), r.Data)
	}
	return r.Template.ExecuteTemplate(resp.BodyWriter(), r.Name, r.Data)
}

// WriteContentType (HTML) writes HTML ContentType.
func (r HTML) WriteContentType(resp *protocol.Response) {
	writeContentType(resp, htmlContentType)
}

type HTMLDebug struct {
	sync.Once
	Template        *template.Template
	RefreshInterval time.Duration
	updateTimeStamp time.Time

	Files   []string
	FuncMap template.FuncMap
	Delims  Delims

	reloadCh chan struct{}
	watcher  *fsnotify.Watcher
}

func (h *HTMLDebug) Instance(name string, data interface{}) Render {
	h.Do(func() {
		h.startChecker()
	})

	select {
	case <-h.reloadCh:
		h.reload()
	default:
	}

	return HTML{
		Template: h.Template,
		Name:     name,
		Data:     data,
	}
}

func (h *HTMLDebug) Close() error {
	if h.watcher == nil {
		return nil
	}
	return h.watcher.Close()
}

func (h *HTMLDebug) reload() {
	h.Template = template.Must(template.New("").
		Delims(h.Delims.Left, h.Delims.Right).
		Funcs(h.FuncMap).
		ParseFiles(h.Files...))
}

func (h *HTMLDebug) startChecker() {
	h.reloadCh = make(chan struct{})

	if h.RefreshInterval > 0 {
		go func() {
			hlog.Debugf("HERTZ[HTMLDebug]: HTML template reloader started with interval %v", h.RefreshInterval)
			for {
				n := time.Now()
				if n.UTC().Sub(h.updateTimeStamp.UTC()) > h.RefreshInterval {
					hlog.Debugf("HERTZ[HTMLDebug]: triggering HTML template reloader")
					h.reloadCh <- struct{}{}
					hlog.Debugf("HERTZ[HTMLDebug]: HTML template has been reloaded, next reload in %v", h.RefreshInterval)
					h.updateTimeStamp = time.Now()
				}
			}
		}()
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	h.watcher = watcher
	for _, f := range h.Files {
		err := watcher.Add(f)
		hlog.Debugf("HERTZ[HTMLDebug]: watching file: %s", f)
		if err != nil {
			hlog.Errorf("HERTZ[HTMLDebug]: add watching file: %s, error happened: %v", f, err)
		}

	}

	go func() {
		hlog.Debugf("HERTZ[HTMLDebug]: HTML template reloader started with file watcher")
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					hlog.Debugf("HERTZ[HTMLDebug]: modified file: %s, html render template will be reloaded at the next rendering", event.Name)
					h.reloadCh <- struct{}{}
					hlog.Debugf("HERTZ[HTMLDebug]: HTML template has been reloaded")
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				hlog.Errorf("HERTZ: error happened when watching the rendering files: %v", err)
			}
		}
	}()
}
