package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"database/sql"

	"github.com/elastic/go-elasticsearch/v6"
	"github.com/elastic/go-elasticsearch/v6/esapi"
	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/olivere/elastic/v6"
	"gopkg.in/go-playground/validator.v9"
	"gopkg.in/ini.v1"
)

var (
	es *elasticsearch.Client
	r  map[string]interface{}
	//配置信息
	iniFile *ini.File

	//elastic的连接
	client   *elastic.Client
	validate *validator.Validate
	host     string
)

func init() {
	var err error
	//加载配置文件
	file, e := ini.Load("conf/app.ini")

	if e != nil {
		checkErr(e)
	}
	iniFile = file
	//读取配置的开发模式
	RunMode := iniFile.Section("").Key("app_mode").String()
	//直接通过函数拼接获取对应模块的url
	host = iniFile.Section(RunMode).Key("host").String()
	fmt.Println(host)

	config := elasticsearch.Config{}
	config.Addresses = []string{host}
	es, err = elasticsearch.NewClient(config)
	fmt.Println("连接es成功")
	checkErr(err)

	res, err := es.Info()
	if err != nil {
		log.Fatalf("Error getting response: %s", err)
	}

	defer res.Body.Close()
	log.Println(res)

	//通过elastic连接
	errorlog := log.New(os.Stdout, "APP", log.LstdFlags)
	var err2 error
	client, err = elastic.NewClient(elastic.SetSniff(false), elastic.SetErrorLog(errorlog), elastic.SetURL(host))
	if err != nil {
		panic(err2)
	}
	info, code, err := client.Ping(host).Do(context.Background())
	if err != nil {
		panic(err2)
	}
	fmt.Printf("Elasticsearch returned with code %d and version %s\n", code, info.Version.Number)

	esversion, err := client.ElasticsearchVersion(host)
	if err != nil {
		panic(err2)
	}
	fmt.Printf("Elasticsearch version %s\n", esversion)
}

func main() {
	//注册路由 router := routers.InitRouter()

	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	//先加载这个页面再使用
	r.LoadHTMLFiles("query.html")
	r.GET("/index", func(c *gin.Context) {
		c.HTML(http.StatusOK, "query.html", gin.H{"title": c.Query("title"), "ce": "123456"})
	})

	r.GET("/search", search)
	r.GET("/searchbak", searchBak)
	r.GET("/add/index", Add)
	r.GET("/create_index", createIndex)
	r.GET("/delete_index", deleteIndex)
	r.GET("/insert_single", insertSingle)
	r.GET("/insert_batch", insertBatch)
	r.GET("/update_single", updateSingle)
	r.GET("/update_query", updateByQuery)
	r.GET("/delete_single", deleteSingle)
	r.GET("/delete_query", deleteByQuery)
	r.GET("/select/by/search", selectBySearch)
	r.GET("/select/course/:title", selectCourse)

	//通过elastic连接的查询
	r.GET("/query/:title", Query)
	r.GET("/back/query", BackQuery)

	//导入数据到es
	r.GET("/insert/course/batch", insertCourseBatch)

	r.Run() // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}

type filterType int

const (
	FILTER_TYPE_TERM filterType = iota
	FILTER_TYPE_RANGE
)

type CommonFilter struct {
	FilterType  filterType
	FilterName  string
	FilterField string
	FilterValue []interface{}
}

//搜索请求参数
type HightLight struct {
	HighlightFields   []string // 列表字段匹配到了关键字则高亮返回，匹配的字词用 HighlightPostTags，HighlightPreTags包裹
	HighlightPostTags string
	HighlightPreTags  string
}

// 搜索请求参数
type CommonSearch struct {
	Index      string             `json:"Index" validate:"required"` // es 索引
	SearchKey  string             // 模糊搜索词
	FieldBoost map[string]float64 // 搜索限定字段及权重, 为空时搜索所有字段，权重默认为 1.0
	Analyzer   string             // 默认 standard
	SortFields map[string]string  // 排序 field -> desc/asc
	Page       int                `json:"Page" validate:"gt=0"`
	PageSize   int                `json:"PageSize" validate:"gt=0"`
	Filters    []*CommonFilter
	*HightLight
}

//搜索
func Query(c *gin.Context) {
	var res *elastic.SearchResult
	var err error
	//取所有
	//res, err = client.Search("course").Type("doc").Do(context.Background())

	title := c.Param("title")

	MatchPhraseQuery1 := elastic.NewMatchQuery("title", title).Operator("and")
	res, err = client.Search("course").Type("doc").Sort("createdTime", false).Size(20).Query(MatchPhraseQuery1).Do(context.Background())

	//短语搜索 搜索about字段中有 rock climbing
	// matchPhraseQuery := elastic.NewMatchPhraseQuery("title", title)
	// res, err = client.Search("course").Type("doc").Query(matchPhraseQuery).Do(context.Background())

	checkErr(err)
	c.JSON(200, res)

}

func BackQuery(c *gin.Context) {
	var res *elastic.SearchResult
	var err error

	//title := c.Param("title")
	title := c.DefaultQuery("title", "")

	subtitle := c.DefaultQuery("subtitle", "")

	if subtitle != "" {
		MatchPhraseQuery1 := elastic.NewMatchQuery("subtitle", subtitle).Operator("and")
		//res, err = client.Search("course_all").Type("back").Query(MatchPhraseQuery1).Do(context.Background())
		res, err = client.Search("course_all").Type("doc").Query(MatchPhraseQuery1).Do(context.Background())
	} else if title != "" {
		MatchPhraseQuery1 := elastic.NewMatchQuery("title", title).Operator("and")
		//res, err = client.Search("course_all").Type("back").Query(MatchPhraseQuery1).Do(context.Background())
		res, err = client.Search("course_all").Type("doc").Sort("createdTime", false).Size(20).Query(MatchPhraseQuery1).Do(context.Background())

	}

	checkErr(err)
	c.JSON(200, res)

}

type Search struct {
	id         int
	categoryId int
	title      string
}

func (r *CommonSearch) Search() (result *elastic.SearchResult, err error) {
	if err = validate.Struct(r); err != nil {
		return
	}

	boolQuery := r.getBoolQuery()

	search := client.Search(r.Index).Query(boolQuery)

	if sorters := getSorters(r.SortFields); sorters != nil {
		search.SortBy(sorters...)
	}
	if highlight := getHighlight(r.HightLight); highlight != nil {
		search.Highlight(highlight)
	}

	offset := (r.Page - 1) * r.PageSize
	resp, err := search.From(offset).Size(r.PageSize).Do(context.Background())

	if err != nil {
		return
	}
	return resp, nil
}

func (r *CommonSearch) getBoolQuery() *elastic.BoolQuery {
	boolQuery := elastic.NewBoolQuery()

	if match := getMatch(r.SearchKey, r.Analyzer, r.FieldBoost); match != nil {
		boolQuery.Must(match)
	}
	if filters := getFilters(r.Filters); filters != nil {
		boolQuery.Filter(filters...)
	}
	return boolQuery
}

// 模糊匹配
func getMatch(searchKey string, analyzer string, fieldBoost map[string]float64) elastic.Query {
	if fieldBoost == nil || len(fieldBoost) <= 0 {
		return nil
	}

	match := elastic.NewMultiMatchQuery(searchKey)
	// 字段查询权重设置
	for f, b := range fieldBoost {
		match.FieldWithBoost(f, b)
	}
	if analyzer != "" && len(analyzer) > 0 {
		match.Analyzer(analyzer)
	}
	return match
}

// 排序设置
func getSorters(sortFields map[string]string) []elastic.Sorter {
	if sortFields == nil || len(sortFields) <= 0 {
		return nil
	}
	sorters := make([]elastic.Sorter, 0)
	for f, s := range sortFields {
		fs := elastic.NewFieldSort(f)
		if strings.ToLower(s) == "desc" {
			fs.Desc()
		} else {
			fs.Asc()
		}
		sorters = append(sorters, fs)
	}
	return sorters
}

// 过滤条件
func getFilters(filters []*CommonFilter) []elastic.Query {
	if filters == nil || len(filters) <= 0 {
		return nil
	}
	var querys = make([]elastic.Query, 0)
	for _, fl := range filters {
		filter := getFilter(fl)
		if filter == nil {
			continue
		}
		querys = append(querys, filter)
	}
	return querys
}

func getFilter(filter *CommonFilter) elastic.Query {
	if len(filter.FilterValue) <= 0 {
		log.Printf("filterField[%s] - filterValue is null.", filter.FilterField)
		return nil
	}
	switch filter.FilterType {
	case FILTER_TYPE_TERM:
		return elastic.NewTermsQuery(filter.FilterField, filter.FilterValue...)
	case FILTER_TYPE_RANGE:
		rangeQuery := elastic.NewRangeQuery(filter.FilterField)
		if len(filter.FilterValue) == 1 {
			rangeQuery.Gte(filter.FilterValue[0])
		} else if len(filter.FilterValue) == 2 {
			rangeQuery.Gte(filter.FilterValue[0]).Lte(filter.FilterValue[1])
		}
		return rangeQuery
	}
	return nil
}

// 高亮设置
func getHighlight(hightlight *HightLight) *elastic.Highlight {
	if hightlight == nil {
		return nil
	}
	hlfs := make([]*elastic.HighlighterField, 0)
	for _, f := range hightlight.HighlightFields {
		hl := elastic.NewHighlighterField(f)
		hlfs = append(hlfs, hl)
	}
	hl := elastic.NewHighlight().Fields(hlfs...).
		PreTags(hightlight.HighlightPreTags).PostTags(hightlight.HighlightPostTags)
	return hl
}

func checkErr(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func search(c *gin.Context) {
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"filter": map[string]interface{}{
					"range": map[string]interface{}{
						"num": map[string]interface{}{
							"gt": 0,
						},
					},
				},
			},
		},
		"size": 0,
		"aggs": map[string]interface{}{
			"num": map[string]interface{}{
				"terms": map[string]interface{}{
					"field": "num",
					//"size":  1,
				},
				"aggs": map[string]interface{}{
					"max_v": map[string]interface{}{
						"max": map[string]interface{}{
							"field": "v",
						},
					},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(query)

	req := esapi.SearchRequest{
		Index:        []string{"course"},
		DocumentType: []string{"doc"},
		Body:         bytes.NewReader(jsonBody),
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	c.JSON(200, req)
}

func searchBak(c *gin.Context) {
	//执行es查询返回json
	var buf bytes.Buffer

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"match": map[string]interface{}{
				"title": "遴选",
			},
		},
	}
	if err := json.NewEncoder(&buf).Encode(query); err != nil {
		log.Fatalf("Error encoding query: %s", err)
	}

	res, err := es.Search(
		es.Search.WithContext(context.Background()),
		es.Search.WithIndex("course"),
		es.Search.WithBody(&buf),
		es.Search.WithTrackTotalHits(true),
		es.Search.WithPretty(),
	)
	if err != nil {
		log.Fatalf("Error getting response: %s", err)
	}
	defer res.Body.Close()

	c.JSON(200, json.NewDecoder(res.Body).Decode(&r))
}

// }

func Add(c *gin.Context) {
	// Build the request body.
	var title string = "Test One"
	var b strings.Builder
	b.WriteString(`{"title" : "`)
	b.WriteString(title)
	b.WriteString(`"}`)

	// Set up the request object.
	req := esapi.IndexRequest{
		Index:      "test",
		DocumentID: strconv.Itoa(1),
		Body:       strings.NewReader(b.String()),
		Refresh:    "true",
	}

	// Perform the request with the client.
	res, err := req.Do(context.Background(), es)
	if err != nil {
		log.Fatalf("Error getting response: %s", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		log.Printf("[%s] Error indexing document ID=%d", res.Status(), 1)
	} else {
		// Deserialize the response into a map.
		var r map[string]interface{}
		if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
			log.Printf("Error parsing the response body: %s", err)
		} else {
			// Print the response status and indexed document version.
			log.Printf("[%s] %s; version=%d", res.Status(), r["result"], int(r["_version"].(float64)))
		}
	}

}

//添加索引
func createIndex(c *gin.Context) {
	body := map[string]interface{}{
		"mappings": map[string]interface{}{
			"test_type": map[string]interface{}{
				"properties": map[string]interface{}{
					"str": map[string]interface{}{
						"type": "keyword", // 表示这个字段不分词
					},
				},
			},
		},
	}
	jsonBody, _ := json.Marshal(body)
	req := esapi.IndicesCreateRequest{
		Index: "test_index",
		Body:  bytes.NewReader(jsonBody),
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

//删除索引
func deleteIndex(c *gin.Context) {
	req := esapi.IndicesDeleteRequest{
		Index: []string{"test_index"},
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

//插入单条数据
func insertSingle(c *gin.Context) {
	body := map[string]interface{}{
		"num": 0,
		"v":   0,
		"str": "test",
	}
	jsonBody, _ := json.Marshal(body)

	req := esapi.CreateRequest{ // 如果是esapi.IndexRequest则是插入/替换
		Index:        "test_index",
		DocumentType: "test_type",
		DocumentID:   "test_1",
		Body:         bytes.NewReader(jsonBody),
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

//批量插入(很明显，也可以批量做其他操作)
func insertBatch(c *gin.Context) {
	var bodyBuf bytes.Buffer
	for i := 2; i < 10; i++ {
		createLine := map[string]interface{}{
			"create": map[string]interface{}{
				"_index": "test_index",
				"_id":    "test_" + strconv.Itoa(i),
				"_type":  "test_type",
			},
		}
		jsonStr, _ := json.Marshal(createLine)
		bodyBuf.Write(jsonStr)
		bodyBuf.WriteByte('\n')

		body := map[string]interface{}{
			"num": i % 3,
			"v":   i,
			"str": "test" + strconv.Itoa(i),
		}
		jsonStr, _ = json.Marshal(body)
		bodyBuf.Write(jsonStr)
		bodyBuf.WriteByte('\n')
	}

	req := esapi.BulkRequest{
		Body: &bodyBuf,
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

func insertCourseBatch(c *gin.Context) {
	var bodyBuf bytes.Buffer
	db, _ := sql.Open("mysql", "root:root@tcp(127.0.0.1:3306)/edusoho?charset=utf8")

	if errConn := db.Ping(); errConn != nil {
		fmt.Println("open database fail")
		return
	}

	if errConn := db.Ping(); errConn != nil {
		fmt.Println("open database fail")
		return
	}

	fmt.Println("connect success")
	defer db.Close()

	//查询出数据表里的url构建urls
	rows, err := db.Query("SELECT id,title,categoryId,createdTime,showMode FROM course_set_v8 where showMode=1 categoryId not in (23, 24, 25)")
	checkErr(err)

	//使用sqlNull***来避免为null情况
	for rows.Next() {
		var id int
		var title string
		var categoryId int
		var createdTime int
		var showMode int

		err = rows.Scan(&id, &title, &categoryId, &createdTime, &showMode)
		checkErr(err)

		fmt.Println(id)
		fmt.Println(title)
		fmt.Println(categoryId)

		createLine := map[string]interface{}{
			"create": map[string]interface{}{
				"_index": "course",
				"_id":    strconv.Itoa(id),
				"_type":  "course_type",
			},
		}

		jsonStr, _ := json.Marshal(createLine)
		bodyBuf.Write(jsonStr)
		bodyBuf.WriteByte('\n')

		body := map[string]interface{}{
			"id":          id,
			"title":       title,
			"categoryId":  categoryId,
			"createdTime": createdTime,
		}
		jsonStr, _ = json.Marshal(body)
		bodyBuf.Write(jsonStr)
		bodyBuf.WriteByte('\n')

	}

	req := esapi.BulkRequest{
		Body: &bodyBuf,
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

//根据id更新
func updateSingle(c *gin.Context) {
	body := map[string]interface{}{
		"doc": map[string]interface{}{
			"v": 100,
		},
	}
	jsonBody, _ := json.Marshal(body)
	req := esapi.UpdateRequest{
		Index:        "test_index",
		DocumentType: "test_type",
		DocumentID:   "test_1",
		Body:         bytes.NewReader(jsonBody),
	}

	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

//根据条件更新
func updateByQuery(c *gin.Context) {
	body := map[string]interface{}{
		"script": map[string]interface{}{
			"lang": "painless",
			"source": `
                ctx._source.v = params.value;
            `,
			"params": map[string]interface{}{
				"value": 101,
			},
		},
		"query": map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
	}
	jsonBody, _ := json.Marshal(body)
	req := esapi.UpdateByQueryRequest{
		Index: []string{"test_index"},
		Body:  bytes.NewReader(jsonBody),
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

//根据id删除
func deleteSingle(c *gin.Context) {
	req := esapi.DeleteRequest{
		Index:        "test_index",
		DocumentType: "test_type",
		DocumentID:   "test_1",
	}

	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

func deleteByQuery(c *gin.Context) {
	body := map[string]interface{}{
		"query": map[string]interface{}{
			"match_all": map[string]interface{}{},
		},
	}
	jsonBody, _ := json.Marshal(body)
	req := esapi.DeleteByQueryRequest{
		Index: []string{"test_index"},
		Body:  bytes.NewReader(jsonBody),
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()
	fmt.Println(res.String())
}

func selectBySearch(c *gin.Context) {
	url := "http://116.62.107.108:9200/course/_search"
	query := []byte(`{
            "query":{
                "bool": {
                    "should": [
                    {"match": {"title": "遴选"}},
                    ]
                }
            }
            }`)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(query))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	fmt.Println(resp.Body)
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s", string(body))
}

func selectCourse(c *gin.Context) {
	//执行es查询返回json
	var buf bytes.Buffer

	title := c.Param("title")

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"match": map[string]interface{}{
				"title": title,
			},
		},
	}
	if err := json.NewEncoder(&buf).Encode(query); err != nil {
		log.Fatalf("Error encoding query: %s", err)
	}

	jsonBody, _ := json.Marshal(query)

	req := esapi.SearchRequest{
		Index:        []string{"course"},
		DocumentType: []string{"course_type"},
		Body:         bytes.NewReader(jsonBody),
	}
	res, err := req.Do(context.Background(), es)
	checkErr(err)
	defer res.Body.Close()

	c.JSON(200, res.String())
}

//同步mysql到es
//go-mysql-elasticsearch
//开启mysql binlog日志，且必须为ROW格式

// <p>通常配置文件都是在mysql的my.cnf，不知道在哪的可以用<br><code>whereis my.cnf</code>
// 找到，然后把<code>binlog_format</code>配置 <code> cat /etc/my.cnf|grep binlog_format</code>修改成ROW，重启！</p>
