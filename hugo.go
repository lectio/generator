package generator

import (
	"fmt"
	"os"
	"time"

	"path/filepath"

	"github.com/gosimple/slug"
	"github.com/lectio/content"
	"github.com/lectio/score"
	"gopkg.in/cheggaaa/pb.v1"
	"gopkg.in/yaml.v2"
)

// HugoGenerator is the primary Hugo content generator engine
type HugoGenerator struct {
	collection           content.Collection
	homePath             string
	contentID            string
	contentPath          string
	scoresStore          score.LinkScoresStore
	simulateSocialScores bool
	verbose              bool
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
	EditorURL         string   `json:"editorURL,omitempty" yaml:"editorURL,omitempty"`

	scores *score.AggregatedLinkScores
}

// NewHugoGenerator creates the default Hugo generation engine
func NewHugoGenerator(collection content.Collection, homePath string, contentID string, createDestPaths bool, verbose bool, simulateSocialScores bool) (*HugoGenerator, error) {
	result := new(HugoGenerator)
	result.collection = collection
	result.homePath = homePath
	result.contentID = contentID
	result.contentPath = filepath.Join(homePath, "content", contentID)
	result.simulateSocialScores = simulateSocialScores
	result.verbose = verbose
	result.createDestPaths = createDestPaths

	scoresStore, ssErr := score.MakeLinkScoresJSONFileStore(filepath.Join(homePath, "data", contentID+"_scores"), filepath.Join(homePath, "data", contentID+"_scores-errors"), createDestPaths)
	if ssErr != nil {
		return nil, ssErr
	}
	result.scoresStore = scoresStore

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

	bodyFrontMatterDescr, ok, _ := source.Body().FrontMatterValue("description")
	if ok {
		switch bodyFrontMatterDescr.(type) {
		case string:
			result.Summary = bodyFrontMatterDescr.(string)
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

	editorURL, _, _ := source.Directive("editorURL")
	result.EditorURL = editorURL.(string)

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
		isIgnored, ignoreReason := resource.IsIgnored()
		if isIgnored {
			g.errors = append(g.errors, fmt.Errorf("ignoring item %d (%q) in HugoGenerator: %v", index, resource.OriginalURLText(), ignoreReason))
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
		result.scores = score.GetAggregatedLinkScores(finalURL, resource.GloballyUniqueKey(), -1, g.simulateSocialScores)

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
		g.scoresStore.Write(hugoContent.scores)
		for _, scorer := range hugoContent.scores.Scores {
			g.scoresStore.Write(scorer)
		}
	}
	ch <- index
}

// GenerateContent executes the engine (creates all the Hugo files from the given collection concurrently)
func (g *HugoGenerator) GenerateContent() error {
	items := g.collection.Content()
	var bar *pb.ProgressBar
	if g.verbose {
		bar = pb.StartNew(len(items))
		bar.ShowCounters = true
	}
	ch := make(chan int)
	for i, source := range items {
		go g.createContentFiles(i, ch, source)
	}
	for i := 0; i < len(items); i++ {
		_ = <-ch
		if g.verbose {
			bar.Increment()
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
