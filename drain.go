package drain

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/hashicorp/golang-lru/simplelru"
)

type Config struct {
	maxNodeDepth    int
	ClusterDepth    int
	SimTh           float64
	MaxChildren     int
	ExtraDelimiters []string
	MaxClusters     int
	ParamPatterns   map[string]*regexp.Regexp
	Tokenizer       func(string) []string
}

type Cluster struct {
	tokens []string
	id     int
	size   int
}

// Tokens returns the tokens of the Cluster.
//
// It does not take any parameters.
// It returns a slice of strings ([]string).
func (c *Cluster) Tokens() []string {
	return c.tokens
}
func (c *Cluster) String() string {
	return fmt.Sprintf("id={%d} : size={%d} : %s", c.id, c.size, strings.Join(c.tokens, " "))
}

func createClusterCache(maxSize int) *ClusterCache {
	if maxSize == 0 {
		maxSize = math.MaxInt
	}
	cache, _ := simplelru.NewLRU(maxSize, nil)
	return &ClusterCache{
		cache: cache,
	}
}

type ClusterCache struct {
	cache simplelru.LRUCache
}

func (c *ClusterCache) Values() []*Cluster {
	values := make([]*Cluster, 0)
	for _, key := range c.cache.Keys() {
		if value, ok := c.cache.Peek(key); ok {
			values = append(values, value.(*Cluster))
		}
	}
	return values
}

func (c *ClusterCache) Set(key int, cluster *Cluster) {
	c.cache.Add(key, cluster)
}

func (c *ClusterCache) Get(key int) *Cluster {
	cluster, ok := c.cache.Get(key)
	if !ok {
		return nil
	}
	return cluster.(*Cluster)
}

func createNode() *Node {
	return &Node{
		keyToChildNode: make(map[string]*Node),
		clusterIDs:     make([]int, 0),
	}
}

type Node struct {
	keyToChildNode map[string]*Node
	clusterIDs     []int
}

func DefaultConfig() *Config {
	return &Config{
		ClusterDepth: 4,
		SimTh:        0.4,
		MaxChildren:  100,
		ParamPatterns: map[string]*regexp.Regexp{
			"*": regexp.MustCompile(`.*`),
		},
		Tokenizer: SpaceTokenizer,
	}
}

func NewConfig(tokenizer func(string) []string, paramPatterns map[string]string) *Config {
	config := DefaultConfig()
	config.Tokenizer = tokenizer
	config.ParamPatterns = make(map[string]*regexp.Regexp)
	for k, v := range paramPatterns {
		config.ParamPatterns[k] = regexp.MustCompile(v)
	}

	return config
}

func SpaceTokenizer(content string) []string {
	content = strings.TrimSpace(content)
	return strings.Split(content, " ")
}

// New initializes and returns a new instance of Drain.
//
// It takes a pointer to a Config struct as a parameter.
// The function panics if the ClusterDepth field of the Config struct is less than 3.
// The function sets the maxNodeDepth field of the Config struct to the ClusterDepth minus 2.
// The function creates a new Drain struct with the specified Config and initializes its fields.
// The function returns the newly created Drain instance.
func New(config *Config) *Drain {
	if config.ClusterDepth < 3 {
		panic("depth argument must be at least 3")
	}
	config.maxNodeDepth = config.ClusterDepth - 2

	d := &Drain{
		config:      config,
		rootNode:    createNode(),
		idToCluster: createClusterCache(config.MaxClusters),
	}
	return d
}

type Drain struct {
	config          *Config
	rootNode        *Node
	idToCluster     *ClusterCache
	clustersCounter int
}

// Clusters returns an array of pointers to Cluster objects.
//
// No parameters.
// Returns an array of pointers to Cluster objects.
func (d *Drain) Clusters() []*Cluster {
	return d.idToCluster.Values()
}

// Train trains the Drain model with the given content and returns the matched cluster.
//
// The content parameter is a string representing the content to be trained.
// The function returns a pointer to a Cluster.
func (d *Drain) Train(content string) *Cluster {
	contentTokens := d.config.Tokenizer(content)

	matchCluster := d.treeSearch(d.rootNode, contentTokens, d.config.SimTh, false)
	// Match no existing cluster
	if matchCluster == nil {
		d.clustersCounter++
		clusterID := d.clustersCounter
		matchCluster = &Cluster{
			tokens: contentTokens,
			id:     clusterID,
			size:   1,
		}
		d.idToCluster.Set(clusterID, matchCluster)
		d.addSeqToPrefixTree(d.rootNode, matchCluster)
	} else {
		newTemplateTokens := d.createTemplate(contentTokens, matchCluster.tokens)
		matchCluster.tokens = newTemplateTokens
		matchCluster.size++
		// Touch cluster to update its state in the cache.
		d.idToCluster.Get(matchCluster.id)
	}
	return matchCluster
}

// Match against an already existing cluster. Match shall be perfect (sim_th=1.0). New cluster will not be created as a result of this call, nor any cluster modifications.
func (d *Drain) Match(content string) *Cluster {
	contentTokens := d.config.Tokenizer(content)
	matchCluster := d.treeSearch(d.rootNode, contentTokens, 1.0, true)
	return matchCluster
}

func (d *Drain) getParamString(token string) string {
	for paramPattern, paramPatternRegexp := range d.config.ParamPatterns {
		if paramPatternRegexp.MatchString(token) {
			return paramPattern
		}
	}
	return ""
}

func (d *Drain) treeSearch(rootNode *Node, tokens []string, simTh float64, includeParams bool) *Cluster {
	tokenCount := len(tokens)

	// at first level, children are grouped by token (word) count
	curNode, ok := rootNode.keyToChildNode[strconv.Itoa(tokenCount)]

	// no template with same token count yet
	if !ok {
		return nil
	}

	// handle case of empty string - return the single cluster in that group
	if tokenCount == 0 {
		return d.idToCluster.Get(curNode.clusterIDs[0])
	}

	// find the leaf node for this - a path of nodes matching the first N tokens (N=tree depth)
	curNodeDepth := 1
	for _, token := range tokens {
		// at max depth
		if curNodeDepth >= d.config.maxNodeDepth {
			break
		}

		// this is last token
		if curNodeDepth == tokenCount {
			break
		}

		keyToChildNode := curNode.keyToChildNode
		curNode, ok = keyToChildNode[token]
		if !ok { // no exact next token exist, try wildcard node
			curNode, ok = keyToChildNode[d.getParamString(token)]
		}
		if !ok { // no wildcard node exist
			return nil
		}
		curNodeDepth++
	}

	// get best match among all clusters with same prefix, or None if no match is above sim_th
	cluster := d.fastMatch(curNode.clusterIDs, tokens, simTh, includeParams)
	return cluster
}

// fastMatch Find the best match for a message (represented as tokens) versus a list of clusters
func (d *Drain) fastMatch(clusterIDs []int, tokens []string, simTh float64, includeParams bool) *Cluster {
	var matchCluster, maxCluster *Cluster

	maxSim := -1.0
	maxParamCount := -1
	for _, clusterID := range clusterIDs {
		// Try to retrieve cluster from cache with bypassing eviction
		// algorithm as we are only testing candidates for a match.
		cluster := d.idToCluster.Get(clusterID)
		if cluster == nil {
			continue
		}
		curSim, paramCount := d.getSeqDistance(cluster.tokens, tokens, includeParams)
		if curSim > maxSim || (curSim == maxSim && paramCount > maxParamCount) {
			maxSim = curSim
			maxParamCount = paramCount
			maxCluster = cluster
		}
	}
	if maxSim >= simTh {
		matchCluster = maxCluster
	}
	return matchCluster
}

func (d *Drain) getSeqDistance(seq1, seq2 []string, includeParams bool) (float64, int) {
	if len(seq1) != len(seq2) {
		panic("seq1 seq2 be of same length")
	}

	simTokens := 0
	paramCount := 0
	for i := range seq1 {
		token1 := seq1[i]
		token2 := seq2[i]
		_, isParamString1 := d.config.ParamPatterns[token1]
		if isParamString1 {
			paramCount++
		} else if token1 == token2 {
			simTokens++
		}
	}
	if includeParams {
		simTokens += paramCount
	}
	retVal := float64(simTokens) / float64(len(seq1))
	return retVal, paramCount
}

func (d *Drain) addSeqToPrefixTree(rootNode *Node, cluster *Cluster) {
	tokenCount := len(cluster.tokens)
	tokenCountStr := strconv.Itoa(tokenCount)

	firstLayerNode, ok := rootNode.keyToChildNode[tokenCountStr]
	if !ok {
		firstLayerNode = createNode()
		rootNode.keyToChildNode[tokenCountStr] = firstLayerNode
	}
	curNode := firstLayerNode

	// handle case of empty string
	if tokenCount == 0 {
		curNode.clusterIDs = append(curNode.clusterIDs, cluster.id)
		return
	}

	currentDepth := 1
	for _, token := range cluster.tokens {
		// if at max depth or this is last token in template - add current cluster to the leaf node
		if (currentDepth >= d.config.maxNodeDepth) || currentDepth >= tokenCount {
			// clean up stale clusters before adding a new one.
			newClusterIDs := make([]int, 0, len(curNode.clusterIDs))
			for _, clusterID := range curNode.clusterIDs {
				if d.idToCluster.Get(clusterID) != nil {
					newClusterIDs = append(newClusterIDs, clusterID)
				}
			}
			newClusterIDs = append(newClusterIDs, cluster.id)
			curNode.clusterIDs = newClusterIDs
			break
		}

		// if token not matched in this layer of existing tree.
		if _, ok = curNode.keyToChildNode[token]; !ok {
			paramString := d.getParamString(token)
			// if token not matched in this layer of existing tree.
			if !d.hasNumbers(token) {
				if _, ok = curNode.keyToChildNode[paramString]; ok {
					if len(curNode.keyToChildNode) < d.config.MaxChildren {
						newNode := createNode()
						curNode.keyToChildNode[token] = newNode
						curNode = newNode
					} else {
						curNode = curNode.keyToChildNode[paramString]
					}
				} else {
					if len(curNode.keyToChildNode)+1 < d.config.MaxChildren {
						newNode := createNode()
						curNode.keyToChildNode[token] = newNode
						curNode = newNode
					} else if len(curNode.keyToChildNode)+1 == d.config.MaxChildren {
						newNode := createNode()
						curNode.keyToChildNode[paramString] = newNode
						curNode = newNode
					} else {
						curNode = curNode.keyToChildNode[paramString]
					}
				}
			} else {
				if _, ok = curNode.keyToChildNode[paramString]; !ok {
					newNode := createNode()
					curNode.keyToChildNode[paramString] = newNode
					curNode = newNode
				} else {
					curNode = curNode.keyToChildNode[paramString]
				}
			}
		} else {
			// if the token is matched
			curNode = curNode.keyToChildNode[token]
		}

		currentDepth++
	}
}

func (d *Drain) hasNumbers(s string) bool {
	for _, c := range s {
		if unicode.IsNumber(c) {
			return true
		}
	}
	return false
}

func (d *Drain) createTemplate(source, target []string) []string {
	if len(source) != len(target) {
		panic("source, target be of same length")
	}
	retVal := make([]string, len(target))
	copy(retVal, target)
	for i := range source {
		if source[i] != target[i] {
			retVal[i] = d.getParamString(source[i])
		}
	}
	return retVal
}
