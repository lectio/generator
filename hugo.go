package generator

import (
	"fmt"
	"io"
	"os"
	"time"

	"path/filepath"

	"github.com/gosimple/slug"
	"github.com/lectio/content"
	"github.com/lectio/link"
	"github.com/lectio/score"
	"gopkg.in/yaml.v2"
)

// ProgressReporter is sent to this package's methods if activity progress reporting is expected
type ProgressReporter interface {
	IsProgressReportingRequested() bool
	StartReportableActivity(expectedItems int)
	StartReportableReaderActivityInBytes(exepectedBytes int64, inputReader io.Reader) io.Reader
	IncrementReportableActivityProgress()
	IncrementReportableActivityProgressBy(incrementBy int)
	CompleteReportableActivityProgress(summary string)
}

// HugoGenerator is the primary Hugo content generator engine
type HugoGenerator struct {
	contentCollection    content.Collection
	scoresCollection     score.Collection
	pr                   ProgressReporter
	homePath             string
	contentID            string
	contentPath          string
	simulateSocialScores bool
	createDestPaths      bool
	errors               []error
}

// HugoContent is a single Hugo page/content
type HugoContent struct {
	Link              string   `json:"link,omitempty" yaml:"link,omitempty"`
	Title             string   `json:"title" yaml:"title"`
	Summary           string   `json:"description" yaml:"description,omitempty"`
	Body              string   `json:"content" yaml:"-"`
	Categories        []string `json:"categories" yaml:"categories,omitempty"`
	CreatedOn         string   `json:"date" yaml:"date"`
	FeaturedImage     string   `json:"featuredimage" yaml:"featuredimage,omitempty"`
	Source            string   `json:"source" yaml:"source,omitempty"`
	Slug              string   `json:"slug" yaml:"slug"`
	GloballyUniqueKey string   `json:"uniquekey" yaml:"uniquekey"`
	SocialScore       int      `json:"socialScore" yaml:"socialScore"`
	EditorURL         string   `json:"editorURL,omitempty" yaml:"editorURL,omitempty"`
}

// NewHugoGenerator creates the default Hugo generation engine
func NewHugoGenerator(contentCollection content.Collection, scoresCollection score.Collection, homePath string, contentID string, createDestPaths bool, pr ProgressReporter, simulateSocialScores bool) (*HugoGenerator, error) {
	result := new(HugoGenerator)
	result.contentCollection = contentCollection
	result.scoresCollection = scoresCollection
	result.homePath = homePath
	result.contentID = contentID
	result.contentPath = filepath.Join(homePath, "content", contentID)
	result.simulateSocialScores = simulateSocialScores
	result.createDestPaths = createDestPaths
	result.pr = pr

	if createDestPaths {
		if _, err := createDirIfNotExist(result.contentPath); err != nil {
			return result, fmt.Errorf("Unable to create content path %q: %v", result.contentPath, err)
		}
	}

	if _, err := os.Stat(result.contentPath); os.IsNotExist(err) {
		return result, fmt.Errorf("content path %q does not exist: %v", result.contentPath, err)
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

	ogTitle, ok := source.Title().OpenGraphTitle(true)
	if ok {
		result.Title = ogTitle
	} else {
		result.Title = source.Title().Clean()
	}

	if source.Body().HasFrontMatter() {
		bodyFrontMatterDescr, ok, descrErr := source.Body().FrontMatter().TextKeyTextValue("description")
		if ok {
			if descrErr != nil {
				g.errors = append(g.errors, fmt.Errorf("error getting description for item %d: %v", index, descrErr))
			} else {
				result.Summary = bodyFrontMatterDescr
			}
		}
	}
	if len(result.Summary) == 0 {
		ogDescr, ok := source.Summary().OpenGraphDescription()
		if ok {
			result.Summary = ogDescr
		} else {
			firstSentence, fsErr := source.Body().FirstSentence()
			if fsErr == nil {
				result.Summary = firstSentence
			} else {
				result.Summary = source.Summary().Original()
			}
		}
	}

	result.Body = source.Body().WithoutFrontMatter()
	result.Categories = source.Categories()
	result.CreatedOn = time.Time(source.CreatedOn()).Format("Mon Jan 2 15:04:05 MST 2006")

	editorURL, ok, eurlErr := source.Directives().MapValue("editorURL")
	if ok {
		if eurlErr != nil {
			g.errors = append(g.errors, fmt.Errorf("error getting editorURL directive for item %d: %v", index, eurlErr))
		} else {
			result.EditorURL = editorURL.(string)
		}
	}

	if source.FeaturedImage() != nil {
		result.FeaturedImage = source.FeaturedImage().String()
	}

	switch v := source.(type) {
	case content.CuratedContent:
		curatedLink := v.Link()
		if curatedLink == nil {
			g.errors = append(g.errors, fmt.Errorf("skipping item %d in HugoGenerator: v.Link() is nil", index))
			return nil
		}
		finalURL, finalURLErr := curatedLink.FinalURL()
		if finalURLErr != nil {
			g.errors = append(g.errors, fmt.Errorf("skipping item %d in HugoGenerator: %v", index, finalURLErr))
			return nil
		}
		result.Link = finalURL.String()
		result.Source = link.GetSimplifiedHostname(finalURL)
		result.Slug = slug.Make(link.GetSimplifiedHostnameWithoutTLD(finalURL) + "-" + source.Title().Clean())
		result.GloballyUniqueKey = curatedLink.GloballyUniqueKey()
		scores := g.scoresCollection.ScoredLink(curatedLink.GloballyUniqueKey())
		if scores != nil {
			result.SocialScore = scores.AggregateSharesCount
		} else {
			g.errors = append(g.errors, fmt.Errorf("unable to find scores for item %d %q in HugoGenerator", index, curatedLink.GloballyUniqueKey()))
		}

	case content.Content:
		result.Slug = slug.Make(source.Title().Clean())
	default:
		fmt.Printf("I don't know about type %T!\n", v)
	}

	return result
}

func (g *HugoGenerator) createContentFiles(index int, ch chan<- int, source content.Content) {
	hugoContent := g.makeHugoContentFromSource(index, source)
	if hugoContent != nil {
		_, err := hugoContent.createContentFile(g)
		if err != nil {
			g.errors = append(g.errors, fmt.Errorf("error writing HugoContent item %d in HugoGenerator: %+v", index, err))
		}
	}
	ch <- index
}

// GenerateContent executes the engine (creates all the Hugo files from the given collection concurrently)
func (g *HugoGenerator) GenerateContent() error {
	items, ccErr := g.contentCollection.Content()
	if ccErr != nil {
		return ccErr
	}

	if g.pr != nil && g.pr.IsProgressReportingRequested() {
		g.pr.StartReportableActivity(len(items))
	}
	ch := make(chan int)
	for i, source := range items {
		go g.createContentFiles(i, ch, source)
	}
	for i := 0; i < len(items); i++ {
		_ = <-ch
		if g.pr != nil && g.pr.IsProgressReportingRequested() {
			g.pr.IncrementReportableActivityProgress()
		}
	}

	if g.pr != nil && g.pr.IsProgressReportingRequested() {
		g.pr.CompleteReportableActivityProgress(fmt.Sprintf("Completed generating Hugo items from %q", g.contentCollection.Source()))
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

	frontMatter, fmErr := yaml.Marshal(c)
	if fmErr != nil {
		return fileName, fmt.Errorf("Unable to marshal front matter %q: %v", fileName, fmErr)
	}

	file.WriteString("---\n")
	_, writeErr := file.Write(frontMatter)
	if writeErr != nil {
		return fileName, fmt.Errorf("Unable to write front matter %q: %v", fileName, writeErr)
	}

	_, writeErr = file.WriteString("---\n" + c.Body)
	if writeErr != nil {
		return fileName, fmt.Errorf("Unable to write content body %q: %v", fileName, writeErr)
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
