package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/k0kubun/pp"

	"github.com/nfnt/resize"

	pigo "github.com/esimov/pigo/core"
	"github.com/fogleman/gg"

	tb "gopkg.in/tucnak/telebot.v2"
)

const boundary = "informs"
const banner = `
┌─┐┬┌─┐┌─┐
├─┘││ ┬│ │
┴  ┴└─┘└─┘

Go (Golang) Face detection library.
    Version: %s

`

// Version indicates the current build version.
var Version string

var (
	// Flags
	telegramToken = flag.String("tg", "", "Telegram API token")
	source        = flag.String("in", "", "Source image")
	destination   = flag.String("out", "", "Destination image")
	cascadeFile   = flag.String("cf", "", "Cascade binary file")
	minSize       = flag.Int("min", 20, "Minimum size of face")
	maxSize       = flag.Int("max", 1000, "Maximum size of face")
	shiftFactor   = flag.Float64("shift", 0.15, "Shift detection window by percentage")
	scaleFactor   = flag.Float64("scale", 1.1, "Scale detection window by percentage")
	angle         = flag.Float64("angle", 0.0, "0.0 is 0 radians and 1.0 is 2*pi radians")
	iouThreshold  = flag.Float64("iou", 0.2, "Intersection over union (IoU) threshold")
	circleMarker  = flag.Bool("circle", false, "Use circle as detection marker")
)
var dc *gg.Context

func main() {
	rand.Seed(time.Now().Unix())

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, fmt.Sprintf(banner, Version))
		flag.PrintDefaults()
	}
	flag.Parse()

	if len(*cascadeFile) == 0 {
		log.Fatal("Usage: go run main.go -cf ../data/facefinder")
	}

	if *scaleFactor < 1 {
		log.Fatal("Scale factor must be greater than 1.")
	}

	if len(*telegramToken) == 0 {
		log.Fatal("Set telegram key! -tg YOUR_KEY")
	}

	b, err := tb.NewBot(tb.Settings{
		Token: *telegramToken,
		// You can also set custom API URL. If field is empty it equals to "https://api.telegram.org"
		Poller: &tb.LongPoller{Timeout: 1 * time.Second},
	})

	if err != nil {
		fmt.Println("telega error")
		log.Fatal(err)
	}

	b.Handle("/start", func(m *tb.Message) {
		b.Send(m.Chat, "Hello! Send me some photo!")
		pp.Println(m)
	})

	b.Handle("/hello", func(m *tb.Message) {
		pp.Println(m)
		mm, err := b.Send(m.Chat, "hello world")
		pp.Println(mm, err)
	})

	b.Handle(tb.OnText, func(m *tb.Message) {
		// pp.Println(m)
	})

	b.Handle(tb.OnPhoto, func(m *tb.Message) {
		pp.Println(m)
		fmt.Println("photo!")

		fileURL, err := b.FileURLByID(m.Photo.FileID)
		if err != nil {
			fmt.Println("FileURLByID fucked up", err)
			return
		}

		fileName, err := downloadTmpFile(fileURL)
		if err != nil {
			fmt.Println("downloadTmpFile fucked up", err)
			return
		}

		// defer os.Remove(fileName)

		// fff := tb.FromURL(fileURL)
		img, err := gg.LoadImage(fileName)
		if err != nil {
			fmt.Println("loadimage fucked up", err)
			return
		}

		fmt.Println("processing")
		processedImage, err := processImage(img)
		fmt.Println("processed")

		if err != nil {
			fmt.Println("No faces detected", err)

			if m.Private() {
				fmt.Println("replying about no faces..")
				b.Send(m.Sender, "ebasos was not detected")
			}

			return
		}

		buff := new(bytes.Buffer)
		err = png.Encode(buff, processedImage)
		if err != nil {
			fmt.Println("failed to create buffer", err)
			return
		}
		processedFileReader := bytes.NewReader(buff.Bytes())

		fmt.Println("replying..")
		// m.ReplyTo(tb.Message{})

		b.Send(m.Chat, &tb.Photo{File: tb.FromReader(processedFileReader)})
		// b.Send(m.Sender, &tb.Message{
		// 	ReplyTo: m,
		// 	Text:    "sasi",
		// 	// Photo:   &tb.Photo{File: tb.FromReader(processedFileReader)},
		// })
		// photos only
	})

	b.Start()
}

func downloadTmpFile(fileURL string) (string, error) {
	resp, err := http.Get(fileURL)

	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	tmpFile, err := ioutil.TempFile("/tmp/", "pepebot.*.png")
	if err != nil {
		return "", err
	}

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func processImage(img image.Image) (image.Image, error) {
	cascadeFile, err := ioutil.ReadFile(*cascadeFile)
	if err != nil {
		fmt.Println("fuck model")
		log.Fatalf("Error reading the cascade file: %v", err)
	}

	p := pigo.NewPigo()
	// Unpack the binary file. This will return the number of cascade trees,
	// the tree depth, the threshold and the prediction from tree's leaf nodes.
	classifier, err := p.Unpack(cascadeFile)
	if err != nil {
		log.Fatalf("Error reading the cascade file: %s", err)
	}

	src := pigo.ImgToNRGBA(img)
	frame := pigo.RgbToGrayscale(src)

	cols, rows := src.Bounds().Max.X, src.Bounds().Max.Y

	cParams := pigo.CascadeParams{
		MinSize:     *minSize,
		MaxSize:     *maxSize,
		ShiftFactor: *shiftFactor,
		ScaleFactor: *scaleFactor,
		ImageParams: pigo.ImageParams{
			Pixels: frame,
			Rows:   rows,
			Cols:   cols,
			Dim:    cols,
		},
	}

	// Run the classifier over the obtained leaf nodes and return the detection results.
	// The result contains quadruplets representing the row, column, scale and detection score.
	dets := classifier.RunCascade(cParams, *angle)

	// Calculate the intersection over union (IoU) of two clusters.
	dets = classifier.ClusterDetections(dets, 0)

	fmt.Println("faces count: ", len(dets))

	if len(dets) <= 0 {
		return nil, errors.New("no faces detected")
	}

	dc = gg.NewContext(cols, rows)
	dc.DrawImage(src, 0, 0)

	processedImage, pepeCount := drawPepe(dets)

	if pepeCount <= 0 {
		return nil, errors.New("no pepos drawn")
	}

	return processedImage, err
}

func drawPepe(detections []pigo.Detection) (image.Image, int) {
	var qThresh float32 = 1.0
	var pepeCount = 0

	for i := 0; i < len(detections); i++ {
		fmt.Println("tresh", detections[i].Q)
		if detections[i].Q > qThresh {
			fmt.Println("pepos drawn!")
			pepeCount++
			dc.DrawImageAnchored(prepareImage(detections[i].Scale), detections[i].Col, detections[i].Row, 0.5, 0.5)
		}
	}

	return dc.Image(), pepeCount
}

func randomInt(min, max int) int {
	return rand.Intn(max-min) + min
}

func prepareImage(targetImageSize int) image.Image {
	loadedImage, err := gg.LoadPNG("pepe_opacity" + strconv.Itoa(randomInt(1, 6)) + ".png")

	if err != nil {
		log.Fatal(err)
		// return image.Image
	}

	// fmt.Println(targetImageSize)

	targetImageSize = int(float64(targetImageSize) * 1.2)

	resizedImage := resize.Resize(uint(targetImageSize), 0, loadedImage, resize.Lanczos3)

	return resizedImage
}
