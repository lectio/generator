package generator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"path/filepath"

	"github.com/gosimple/slug"
	"github.com/lectio/content"
	"github.com/lectio/score"
	"gopkg.in/cheggaaa/pb.v1"
)

const facebookGraphFileExtn = ".facebook.json"
const facebookGraphErrorFileExtn = ".facebook-error.json"
const linkedInCountFileExtn = ".linkedin.json"
const linkedInCountErrorFileExtn = ".linkedin-error.json"

// HugoGenerator is the primary Hugo content generator engine
type HugoGenerator struct {
	collection           content.Collection
	homePath             string
	contentID            string
	contentPath          string
	scoresDataPath       string
	simulateSocialScores bool
	verbose              bool
	createDestPaths      bool
	errors               []error
}

// HugoContentTime is a convenience type for storing content timestamp
type HugoContentTime time.Time

// MarshalJSON stores HugoContentTime in a manner readable by Hugo
func (hct HugoContentTime) MarshalJSON() ([]byte, error) {
	stamp := fmt.Sprintf("\"%s\"", time.Time(hct).Format("Mon Jan 2 15:04:05 MST 2006"))
	return []byte(stamp), nil
}

// HugoContent is a single Hugo page/content
type HugoContent struct {
	Link              string          `json:"link,omitempty"`
	Title             string          `json:"title"`
	Summary           string          `json:"description"`
	Body              string          `json:"content"`
	Categories        []string        `json:"categories"`
	CreatedOn         HugoContentTime `json:"date"`
	FeaturedImage     string          `json:"featuredimage"`
	Source            string          `json:"source"`
	Slug              string          `json:"slug"`
	GloballyUniqueKey string          `json:"uniquekey"`
	TotalSharesCount  int             `json:"totalSharesCount"`

	scores score.LinkScores
}

// NewHugoGenerator creates the default Hugo generation engine
func NewHugoGenerator(collection content.Collection, homePath string, contentID string, createDestPaths bool, verbose bool, simulateSocialScores bool) (*HugoGenerator, error) {
	result := new(HugoGenerator)
	result.collection = collection
	result.homePath = homePath
	result.contentID = contentID
	result.contentPath = filepath.Join(homePath, "content", contentID)
	result.scoresDataPath = filepath.Join(homePath, "data", "scores", contentID)
	result.simulateSocialScores = simulateSocialScores
	result.verbose = verbose
	result.createDestPaths = createDestPaths

	if createDestPaths {
		if _, err := createDirIfNotExist(result.contentPath); err != nil {
			return result, fmt.Errorf("Unable to create content path %q: %v", result.contentPath, err)
		}
		if _, err := createDirIfNotExist(result.scoresDataPath); err != nil {
			return result, fmt.Errorf("Unable to create scores data path %q: %v", result.scoresDataPath, err)
		}
	}

	if _, err := os.Stat(result.contentPath); os.IsNotExist(err) {
		return result, fmt.Errorf("content path %q does not exist: %v", result.contentPath, err)
	}
	if _, err := os.Stat(result.scoresDataPath); os.IsNotExist(err) {
		return result, fmt.Errorf("scores data path %q does not exist: %v", result.scoresDataPath, err)
	}

	return result, nil
}

// Errors records any issues with the generator as its processing its entries
func (g HugoGenerator) Errors() []error {
	return g.errors
}

// GetContentFilename returns the name of the file a given piece of HugoContent
func (g HugoGenerator) GetContentFilename(gc *HugoContent) string {
	return fmt.Sprintf("%s.md", filepath.Join(g.contentPath, gc.Slug))
}

func (g *HugoGenerator) makeHugoContentFromSource(index int, source content.Content) *HugoContent {
	result := new(HugoContent)

	ogTitle, ok := source.Title().OpenGraphTitle()
	if ok {
		result.Title = ogTitle
	} else {
		result.Title = source.Title().Clean()
	}

	ogDescr, ok := source.Summary().OpenGraphDescription()
	if ok {
		result.Summary = ogDescr
	} else {
		firstSentence, fsErr := source.Summary().FirstSentenceOfBody()
		if fsErr == nil {
			result.Summary = firstSentence
		} else {
			result.Summary = source.Summary().Original()
		}
	}

	result.Body = source.Body()
	result.Categories = source.Categories()
	result.CreatedOn = HugoContentTime(source.CreatedOn())
	if source.FeaturedImage() != nil {
		result.FeaturedImage = source.FeaturedImage().String()
	}

	switch v := source.(type) {
	case content.CuratedContent:
		resource := v.TargetResource()
		if resource == nil {
			g.errors = append(g.errors, fmt.Errorf("skipping item %d in HugoGenerator, it has nil TargetResource()", index))
			return nil
		}
		isURLValid, isDestValid := resource.IsValid()
		if !isURLValid || !isDestValid {
			g.errors = append(g.errors, fmt.Errorf("skipping item %d due to invalid resource URL %q; isURLValid: %v, isDestValid: %v", index, resource.OriginalURLText(), isURLValid, isDestValid))
			return nil
		}
		finalURL, _, _ := resource.GetURLs()
		if finalURL == nil || len(finalURL.String()) == 0 {
			g.errors = append(g.errors, fmt.Errorf("skipping item %d in HugoGenerator, finalURL is nil or empty string", index))
			return nil
		}
		result.Link = finalURL.String()
		result.Source = content.GetSimplifiedHostname(finalURL)
		result.Slug = slug.Make(content.GetSimplifiedHostnameWithoutTLD(finalURL) + "-" + source.Title().Clean())
		result.GloballyUniqueKey = resource.GloballyUniqueKey()
		result.scores = score.GetLinkScores(finalURL, resource.GloballyUniqueKey(), score.DefaultInitialTotalSharesCount, g.simulateSocialScores)
		result.TotalSharesCount = result.scores.TotalSharesCount()

	case content.Content:
		result.Slug = slug.Make(source.Title().Clean())
	default:
		fmt.Printf("I don't know about type %T!\n", v)
	}

	return result
}

// GenerateContent executes the engine (creates all the Hugo files from the given collection)
func (g *HugoGenerator) GenerateContent() error {
	items := g.collection.Content()
	var bar *pb.ProgressBar
	if g.verbose {
		bar = pb.StartNew(len(items))
		bar.ShowCounters = true
	}
	for i, source := range items {
		hugoContent := g.makeHugoContentFromSource(i, source)
		if hugoContent != nil {
			_, err := hugoContent.createContentFile(g)
			if err != nil {
				g.errors = append(g.errors, fmt.Errorf("error writing HugoContent item %d in HugoGenerator: %+v", i, err))
			}
		}
		if g.verbose {
			bar.Increment()
		}

		if hugoContent.scores != nil {
			hugoContent.createScorerDataFile(g.scoresDataPath, hugoContent.scores.FacebookLinkScore())
			hugoContent.createScorerDataFile(g.scoresDataPath, hugoContent.scores.LinkedInLinkScore())
		}
	}
	if g.verbose {
		bar.FinishPrint(fmt.Sprintf("Completed generating Hugo items from %q", g.collection.Source()))
	}

	return nil
}

func (c *HugoContent) createContentFile(g *HugoGenerator) (string, error) {
	fileName := g.GetContentFilename(c)
	file, createErr := os.Create(fileName)
	if createErr != nil {
		return fileName, fmt.Errorf("Unable to create file %q: %v", fileName, createErr)
	}
	defer file.Close()

	frontMatter, fmErr := json.MarshalIndent(c, "", "	")
	if fmErr != nil {
		return fileName, fmt.Errorf("Unable to marshal front matter %q: %v", fileName, fmErr)
	}

	_, writeErr := file.Write(frontMatter)
	if writeErr != nil {
		return fileName, fmt.Errorf("Unable to write front matter %q: %v", fileName, writeErr)
	}

	_, writeErr = file.WriteString("\n\n" + c.Body)
	if writeErr != nil {
		return fileName, fmt.Errorf("Unable to write content body %q: %v", fileName, writeErr)
	}

	return fileName, nil
}

func (c *HugoContent) createScorerDataFile(path string, scorer score.LinkScorer) (string, error) {
	if scorer == nil {
		return "", errors.New("Unable to create data file, scorer is nil")
	}
	suffix, _ := scorer.Names()
	if !scorer.IsValid() {
		suffix = suffix + "-error"
	}
	fileName := fmt.Sprintf("%s.%s.json", filepath.Join(path, c.GloballyUniqueKey), suffix)
	file, createErr := os.Create(fileName)
	if createErr != nil {
		return fileName, fmt.Errorf("Unable to create data file %q: %v", fileName, createErr)
	}
	defer file.Close()

	frontMatter, fmErr := json.MarshalIndent(scorer, "", "	")
	if fmErr != nil {
		return fileName, fmt.Errorf("Unable to marshal data into JSON %q: %v", fileName, fmErr)
	}

	_, writeErr := file.Write(frontMatter)
	if writeErr != nil {
		return fileName, fmt.Errorf("Unable to write data %q: %v", fileName, writeErr)
	}

	return fileName, nil
}

// createDirIfNotExist creates a path if it does not exist. It is similar to mkdir -p in shell command,
// which also creates parent directory if not exists.
func createDirIfNotExist(dir string) (bool, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0755)
		return true, err
	}
	return false, nil
}
