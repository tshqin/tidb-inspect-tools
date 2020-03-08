// Copyright 2018 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

/*
   Copyright 2016 Vastech SA (PTY) LTD

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/ngaut/log"
	"github.com/pborman/uuid"
	"github.com/pingcap/tidb-inspect-tools/grafana_collector/config"
	"github.com/pingcap/tidb-inspect-tools/grafana_collector/grafana"
	"github.com/pkg/errors"
	"github.com/signintech/gopdf"
)

var (
	cfg = config.GetGlobalConfig()

	// FontDir ... ttf font directory
	FontDir = ""
)

const (
	imgDir    = "images"
	reportPdf = "report.pdf"
)

// Report groups functions related to genrating the report.
// After reading and closing the pdf returned by Generate(),
// call Clean() to delete the pdf file as well the temporary build files
type Report interface {
	Generate() (pdf io.ReadCloser, err error)
	Clean()
}

type report struct {
	gClient  grafana.Client
	time     grafana.TimeRange
	dashName string
	tmpDir   string
}

// SetFontDir ... sets up ttf font directory
func SetFontDir(fontDir string) {
	FontDir = fontDir
}

// New ... creates a new Report
func New(g grafana.Client, dashName string, timeRange grafana.TimeRange) Report {
	return new(g, dashName, timeRange)
}

func new(g grafana.Client, dashName string, timeRange grafana.TimeRange) *report {
	tmpDir := filepath.Join("tmp", uuid.New())
	return &report{g, timeRange, dashName, tmpDir}
}

// Generate returns the report.pdf file. After reading this file it should be Closed()
// After closing the file, call report.Clean() to delete the file
func (rep *report) Generate() (pdf io.ReadCloser, err error) {
	// prepare stage: fetch dashboard json and create image directory
	dash, err := rep.gClient.GetDashboard(rep.dashName)
	if err != nil {
		return nil, errors.Errorf("fetching dashboard %s error: %v", rep.dashName, err)
	}

	err = os.MkdirAll(rep.imgDirPath(), 0777)
	if err != nil {
		return nil, errors.Errorf("creating image directory %s error: %v", rep.imgDirPath(), err)
	}

	// working stage：fetch panel images
	err = rep.renderPNGsParallel(dash)
	if err != nil {
		return nil, errors.Errorf("rendering PNGs in parallel for dash %+v error: %v. It is recommended to select time range within 6 hours on the Dashboard. Otherwise, the grafana timeout problem might occur.", dash, err)
	}

	// working stage：render panel images to pdf
	pdf, err = rep.renderPDF(dash)
	if err != nil {
		return nil, errors.Errorf("rendering pdf for dash %+v error: %v", dash, err)
	}
	return pdf, nil
}

// Clean deletes the temporary directory used during report generation
func (rep *report) Clean() {
	err := os.RemoveAll(rep.tmpDir)
	if err != nil {
		log.Errorf("cleaning up tmp dir %s error: %v", rep.tmpDir, err)
	}
}

func (rep *report) imgDirPath() string {
	return filepath.Join(rep.tmpDir, imgDir)
}

func (rep *report) pdfPath() string {
	return filepath.Join(rep.tmpDir, reportPdf)
}

func (rep *report) renderPNGsParallel(dash grafana.Dashboard) error {
	//buffer all panels on a channel
	panels := make(chan grafana.Panel, len(dash.Panels))
	for _, p := range dash.Panels {
		panels <- p
	}
	close(panels)

	//fetch images in parrallel form Grafana sever.
	//limit concurrency using a worker pool to avoid overwhelming grafana
	//for dashboards with many panels.
	var (
		wg      sync.WaitGroup
		workers = 5
		errs    = make(chan error, len(dash.Panels)) //routines can return errors on a channel
	)

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(panels <-chan grafana.Panel, errs chan<- error) {
			defer wg.Done()
			for p := range panels {
				err := rep.renderPNG(p)
				if err != nil {
					log.Errorf("creating image for panel ID %d error: %v", p.ID, err)
					errs <- err
				}
			}
		}(panels, errs)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (rep *report) imgFilePath(p grafana.Panel) string {
	imgFileName := fmt.Sprintf("image%d.png", p.ID)
	imgFilePath := filepath.Join(rep.imgDirPath(), imgFileName)
	return imgFilePath
}

func (rep *report) renderPNG(p grafana.Panel) error {
	body, err := rep.gClient.GetPanelPng(p, rep.dashName, rep.time)
	if err != nil {
		return errors.Errorf("getting panel %+v error: %v", p, err)
	}
	defer body.Close()

	imgPath := rep.imgFilePath(p)
	file, err := os.Create(imgPath)
	if err != nil {
		return errors.Errorf("creating image file %s error: %v", imgPath, err)
	}
	defer file.Close()

	_, err = io.Copy(file, body)
	if err != nil {
		return errors.Errorf("copying body to file error: %v", err)
	}
	return nil
}

// NewPDF ... creates a new PDF and sets font
func (rep *report) NewPDF() (*gopdf.GoPdf, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: cfg.Rect["page"].Width, H: cfg.Rect["page"].Height}})

	ttfPath := FontDir + cfg.Font.Ttf
	err := pdf.AddTTFFont(cfg.Font.Family, ttfPath)
	if err != nil {
		log.Errorf("add ttf font error: %v", err)
		return nil, errors.Wrap(err, "add ttf font")
	}

	err = pdf.SetFont(cfg.Font.Family, "", cfg.Font.Size)
	if err != nil {
		log.Errorf("set font error: %v", err)
		return nil, errors.Wrap(err, "set font")
	}

	return pdf, nil
}

// createHomePage ... add Home Page for PDF
func (rep *report) createHomePage(pdf *gopdf.GoPdf, dash grafana.Dashboard) {
	pdf.AddPage()
	pdf.SetX(cfg.Position.X)
	pdf.Cell(nil, "Dashboard: "+dash.Title)
	pdf.Br(cfg.Position.Br)
	pdf.SetX(cfg.Position.X)
	pdf.Cell(nil, rep.time.FromFormatted()+" to "+rep.time.ToFormatted())
}

func (rep *report) renderPDF(dash grafana.Dashboard) (outputPDF *os.File, err error) {
	log.Infof("PDF templates config: %+v\n", cfg)

	pdf, err := rep.NewPDF()
	if err != nil {
		return nil, errors.Wrap(err, "new pdf file")
	}
	rep.createHomePage(pdf, dash)

	// setting rectangle size for grafana panel type: Graph/Singlestat
	rectGraph := &gopdf.Rect{W: cfg.Rect["graph"].Width, H: cfg.Rect["graph"].Height}
	rectSinglestat := &gopdf.Rect{W: cfg.Rect["singlestat"].Width, H: cfg.Rect["singlestat"].Height}
	rect := &gopdf.Rect{}

	var count int
	for _, p := range dash.Panels {
		imgPath := rep.imgFilePath(p)

		if p.IsSingleStat() {
			rect = rectSinglestat
		} else {
			rect = rectGraph
		}

		// Add two images on every page
		if count%2 == 0 {
			pdf.SetX(cfg.Position.X)
			pdf.SetY(cfg.Position.TitleY1)
			pdf.Cell(nil, fmt.Sprintf("Row: %s, Panel: %s", p.RowTitle, p.Title))
			err = pdf.Image(imgPath, cfg.Position.X, cfg.Position.ImageY1, rect)
		} else {
			pdf.SetX(cfg.Position.X)
			pdf.SetY(cfg.Position.TitleY2)
			pdf.Cell(nil, fmt.Sprintf("Row: %s, Panel: %s", p.RowTitle, p.Title))
			err = pdf.Image(imgPath, cfg.Position.X, cfg.Position.ImageY2, rect)
			pdf.AddPage()
		}
		if err != nil {
			log.Errorf("rendering image %s to PDF error: %v", imgPath, err)
		} else {
			log.Infof("rendering image to PDF: %s", imgPath)
		}
		count++
	}

	// WritePdf(pdfPath string) func in gopdf doesn't return error
	pdf.WritePdf(rep.pdfPath())
	outputPDF, err = os.Open(rep.pdfPath())
	return outputPDF, errors.Wrap(err, "open pdf file")
}
