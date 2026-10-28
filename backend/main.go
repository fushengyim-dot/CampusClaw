package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"campusclaw/backend/config"
	"campusclaw/backend/database"
	"campusclaw/backend/handlers"
	"campusclaw/backend/mcpserver"
	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Getenv, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, getenv func(string) string, args []string, out, errOut io.Writer) int {
	if len(args) == 1 && args[0] == "mcp-server" {
		return runMCPServer(ctx, getenv, out, errOut)
	}
	baseGetenv := getenv
	getenv = func(key string) string {
		if strings.HasPrefix(key, "SEED_") {
			file := baseGetenv(key + "_FILE")
			if file == "" {
				file = os.Getenv(key + "_FILE")
			}
			if file != "" {
				data, err := os.ReadFile(file)
				if err != nil {
					return ""
				}
				return strings.TrimRight(string(data), "\r\n")
			}
		}
		return baseGetenv(key)
	}
	checkOnly := len(args) == 1 && args[0] == "--check-config"
	checkHealth := len(args) == 1 && args[0] == "--healthcheck"
	checkDB := len(args) == 1 && args[0] == "--check-db"
	initSchema := len(args) == 1 && args[0] == "--init-schema"
	initAccounts := len(args) == 1 && args[0] == "--init-accounts"
	testEmbedding := len(args) == 1 && args[0] == "--test-embedding"
	testChat := len(args) == 1 && args[0] == "--test-chat"
	checkQdrantTest := len(args) == 1 && args[0] == "--check-qdrant-test"
	checkQdrantPersistence := len(args) == 1 && args[0] == "--check-qdrant-persistence"
	if checkQdrantTest || checkQdrantPersistence {
		return checkLocalQdrant(ctx, getenv("QDRANT_URL"), checkQdrantTest, out, errOut)
	}
	if len(args) != 0 && !checkOnly && !checkHealth && !checkDB && !initSchema && !initAccounts && !testEmbedding && !testChat {
		fmt.Fprintln(errOut, "参数错误：命令不受支持")
		return 1
	}
	if testEmbedding {
		dimensions, err := strconv.Atoi(getenv("EMBEDDING_DIM"))
		if err != nil {
			fmt.Fprintln(errOut, "嵌入检查失败：配置不完整或格式错误")
			return 1
		}
		client, err := services.NewEmbeddingClient(services.EmbeddingConfig{BaseURL: getenv("EMBEDDING_BASE_URL"), APIKey: getenv("EMBEDDING_API_KEY"), Model: getenv("EMBEDDING_MODEL"), Dimensions: dimensions})
		if err != nil {
			fmt.Fprintln(errOut, "嵌入检查失败：配置不完整或格式错误")
			return 1
		}
		vector, err := client.EmbedQuery(ctx, "CampusClaw 课程网关连通性检查。")
		if err != nil {
			var diagnostic *services.EmbeddingDiagnostic
			if errors.As(err, &diagnostic) {
				if diagnostic.HTTPStatus != 0 {
					fmt.Fprintf(errOut, "嵌入检查失败：%s（HTTP %d）；密钥和响应正文未显示\n", diagnostic.Category, diagnostic.HTTPStatus)
				} else {
					fmt.Fprintf(errOut, "嵌入检查失败：%s；密钥和响应正文未显示\n", diagnostic.Category)
				}
			} else {
				fmt.Fprintln(errOut, "嵌入检查失败：服务暂不可用；密钥和响应正文未显示")
			}
			return 1
		}
		fmt.Fprintf(out, "课程嵌入接口连通检查通过；返回维度=%d；仅发送一条自拟短句。\n", len(vector))
		return 0
	}
	if testChat {
		client, err := services.NewChatClient(services.ChatConfig{BaseURL: getenv("CHAT_BASE_URL"), APIKey: getenv("CHAT_API_KEY"), Model: getenv("CHAT_MODEL")})
		if err != nil {
			fmt.Fprintln(errOut, "对话检查失败：配置不完整或格式错误；未显示密钥和响应正文")
			return 1
		}
		messages := []services.ChatMessage{
			{Role: "system", Content: "你正在进行接口连通性检查。只根据给定的合成资料回答，不要猜测；用一句中文回答，并在句末标注[1]。"},
			{Role: "user", Content: "合成资料[1]：CampusClaw是一个供软件开发课程教师和学生使用的学习平台。\n合成问题：CampusClaw供谁使用？"},
		}
		if _, err := client.Complete(ctx, messages); err != nil {
			fmt.Fprintln(errOut, "课程对话接口检查失败：请求未通过或返回格式不符；密钥和响应正文未显示；没有自动重试")
			return 1
		}
		fmt.Fprintln(out, "课程对话接口连通检查通过；只发送一条自拟问题和一段合成资料；回答正文未显示。")
		return 0
	}
	if checkHealth {
		port := getenv("API_PORT")
		if port == "" {
			port = "8080"
		}
		client := http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://127.0.0.1:" + port + "/health")
		if err != nil {
			return 1
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return 1
		}
		return 0
	}
	cfg, err := config.Load(getenv)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if checkOnly {
		fmt.Fprintln(out, "配置格式检查通过；尚未连接数据库，不代表服务已就绪。")
		return 0
	}
	if checkDB || initSchema || initAccounts {
		db, err := database.Open(ctx, cfg)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		defer db.Close()
		if initAccounts {
			if err := database.InitSchema(ctx, db); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			if err := database.InitAccounts(ctx, db, getenv); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			fmt.Fprintln(out, "A/B班与四个演示账号已准备；已有账号密码保持不变，登录上传尚未实现。")
			return 0
		}
		if initSchema {
			if err := database.InitSchema(ctx, db); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			fmt.Fprintln(out, "数据库结构版本检查通过；未创建演示账号，未修改已有业务记录。")
			return 0
		}
		fmt.Fprintln(out, "数据库连接与SELECT 1检查通过；本命令不检查业务初始化，不代表平台就绪。")
		return 0
	}
	// 仅本机开发入口。Compose内部监听地址在部署步骤另行配置。
	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.APIListenHost, strconv.Itoa(cfg.APIPort)))
	if err != nil {
		fmt.Fprintln(errOut, "启动失败：API_PORT 无法监听，请检查端口占用")
		return 1
	}
	defer listener.Close()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		fmt.Fprintln(errOut, err)
		fmt.Fprintln(out, "后端检查入口保持运行；数据库不可用时健康状态和业务接口返回暂不可用。")
		return serve(ctx, listener, handlers.New(), out, errOut)
	}
	defer db.Close()
	login, err := services.NewAuth(repositories.NewSessions(db), cfg.SessionSecret, nil)
	if err != nil {
		fmt.Fprintln(errOut, "账号核对服务初始化失败")
		return 1
	}
	materialService, err := services.NewMaterials(db, os.Getenv("UPLOAD_ROOT"))
	if err != nil {
		fmt.Fprintln(errOut, "讲义存储初始化失败")
		return 1
	}
	startupReady := true
	if err := database.InitSchema(ctx, db); err != nil {
		startupReady = false
		fmt.Fprintln(errOut, "数据表检查未通过；服务保持暂不可用")
	} else if err := database.InitAccounts(ctx, db, getenv); err != nil {
		startupReady = false
		fmt.Fprintln(errOut, "演示账号检查未通过；服务保持暂不可用")
	}
	if startupReady {
		if err := materialService.Recover(ctx); err != nil {
			startupReady = false
		}
	}
	if startupReady {
		seedRoot := os.Getenv("MATERIAL_SEED_ROOT")
		if err := materialService.EnsureCourseMaterials(ctx, seedRoot); err != nil {
			startupReady = false
			fmt.Fprintln(errOut, "课程样例材料初始化未通过；已有材料不会被覆盖")
		}
	}
	if startupReady {
		if err := services.NewKnowledge(db).EnsureChunks(ctx); err != nil {
			startupReady = false
			fmt.Fprintln(errOut, "课程材料切片检查未通过；已有材料不会被覆盖")
		}
	}
	lesson5 := services.NewLesson5Store(db)
	mcpRegistry, mcpErr := services.NewMCPRegistry(db, cfg.SessionSecret)
	if mcpErr != nil {
		startupReady = false
	}
	if startupReady {
		if err := lesson5.EnsureSeedSkills(ctx); err != nil {
			startupReady = false
			fmt.Fprintln(errOut, "第5课预置技能检查未通过；已有配置不会被覆盖")
		}
	}
	if startupReady {
		if err := lesson5.EnsureBuiltinAgents(ctx); err != nil {
			startupReady = false
			fmt.Fprintln(errOut, "三个内置应用初始化未通过；已有数据不会被覆盖")
		}
	}
	if startupReady && getenv("MCP_PRESET_TOKEN_FILE") != "" {
		secret, e := os.ReadFile(getenv("MCP_PRESET_TOKEN_FILE"))
		if e != nil || mcpRegistry.EnsureBuiltIn(ctx, strings.TrimRight(string(secret), "\r\n")) != nil {
			startupReady = false
			fmt.Fprintln(errOut, "内置MCP服务器凭据检查未通过；工具接入暂不可用")
		}
	}
	if !startupReady {
		materialService.SetUnavailable()
		fmt.Fprintln(errOut, "讲义数据检查未通过；网站仍可打开，但健康状态为暂不可用，材料接口会拒绝访问")
	}
	knowledge := services.NewKnowledge(db)
	baseURL, apiKey, model, dimText := getenv("EMBEDDING_BASE_URL"), getenv("EMBEDDING_API_KEY"), getenv("EMBEDDING_MODEL"), getenv("EMBEDDING_DIM")
	if baseURL != "" || apiKey != "" || model != "" || dimText != "" {
		dimensions, dimErr := strconv.Atoi(dimText)
		embedder, embedErr := services.NewEmbeddingClient(services.EmbeddingConfig{BaseURL: baseURL, APIKey: apiKey, Model: model, Dimensions: dimensions})
		qdrant, qdrantErr := services.NewQdrant(getenv("QDRANT_URL"))
		if dimErr == nil && embedErr == nil && qdrantErr == nil && retryQdrant(ctx, func() error { return qdrant.EnsureCollection(ctx, "campusclaw_chunks", dimensions) }) == nil {
			knowledge = services.NewKnowledgeWithVector(db, qdrant, embedder, "campusclaw_chunks", dimensions)
		} else {
			fmt.Fprintln(errOut, "嵌入或向量库配置不可用；关键词检索仍可使用，语义检索暂不可用")
		}
	}
	var chat services.ChatCompleter
	chatBase, chatKey, chatModel := getenv("CHAT_BASE_URL"), getenv("CHAT_API_KEY"), getenv("CHAT_MODEL")
	if chatBase != "" || chatKey != "" || chatModel != "" {
		chatClient, chatErr := services.NewChatClient(services.ChatConfig{BaseURL: chatBase, APIKey: chatKey, Model: chatModel})
		if chatErr == nil {
			chat = chatClient
		} else {
			fmt.Fprintln(errOut, "对话服务配置不可用；资料检索仍可使用，AI问答暂不可用")
		}
	}
	return serve(ctx, listener, handlers.NewApplicationWithLesson6(login, materialService, cfg.AppOrigin, knowledge, chat, lesson5, mcpRegistry), out, errOut)
}

func runMCPServer(ctx context.Context, getenv func(string) string, out, errOut io.Writer) int {
	tokenPath := getenv("MCP_SERVER_TOKEN_FILE")
	if tokenPath == "" {
		fmt.Fprintln(errOut, "MCP服务配置不完整：缺少令牌文件路径")
		return 1
	}
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		fmt.Fprintln(errOut, "MCP服务配置不完整：无法读取令牌文件")
		return 1
	}
	server, err := mcpserver.New(strings.TrimRight(string(tokenBytes), "\r\n"))
	if err != nil {
		fmt.Fprintln(errOut, "MCP服务配置不完整：令牌格式无效")
		return 1
	}
	host := getenv("MCP_SERVER_LISTEN_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := getenv("MCP_SERVER_PORT")
	if port == "" {
		port = "8091"
	}
	if (host != "127.0.0.1" && host != "0.0.0.0") || !validPort(port) {
		fmt.Fprintln(errOut, "MCP服务配置格式错误：监听地址或端口不正确")
		return 1
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		fmt.Fprintln(errOut, "MCP服务启动失败：监听端口不可用")
		return 1
	}
	defer listener.Close()
	fmt.Fprintln(out, "本地MCP工具服务已启动；只提供内置只读课程工具。")
	return serve(ctx, listener, server.Handler(), io.Discard, errOut)
}

func validPort(value string) bool {
	n, err := strconv.Atoi(value)
	return err == nil && n > 0 && n <= 65535 && strconv.Itoa(n) == value
}

func checkLocalQdrant(ctx context.Context, endpoint string, write bool, out, errOut io.Writer) int {
	client, err := services.NewQdrant(endpoint)
	if err != nil {
		fmt.Fprintln(errOut, "Qdrant验收失败：服务地址配置无效")
		return 1
	}
	const collection = "campusclaw_test_chunks"
	const dims = 4
	if write {
		if err := retryQdrant(ctx, func() error { return client.EnsureCollection(ctx, collection, dims) }); err != nil {
			fmt.Fprintln(errOut, "Qdrant验收失败：测试集合不可用或维度不匹配")
			return 1
		}
		points := []services.VectorPoint{
			{ID: 910000000000001, ChunkID: 910000000000001, ClassID: 810001, MaterialID: 710001, EntryID: 610001, Vector: []float32{1, 0, 0, 0}},
			{ID: 910000000000002, ChunkID: 910000000000002, ClassID: 810002, MaterialID: 710002, EntryID: 610002, Vector: []float32{0.9, 0.1, 0, 0}},
		}
		if err := client.Upsert(ctx, collection, points, dims); err != nil {
			fmt.Fprintln(errOut, "Qdrant验收失败：合成向量写入未完成")
			return 1
		}
	}
	var hits []services.VectorHit
	err = retryQdrant(ctx, func() error {
		var searchErr error
		hits, searchErr = client.Search(ctx, collection, []float32{1, 0, 0, 0}, 810001, 10, 0, dims)
		return searchErr
	})
	if err != nil || len(hits) == 0 || hits[0].ChunkID != 910000000000001 {
		fmt.Fprintln(errOut, "Qdrant验收失败：查询未返回预期的本班合成切片")
		return 1
	}
	for _, hit := range hits {
		if hit.ChunkID == 910000000000002 {
			fmt.Fprintln(errOut, "Qdrant验收失败：跨班合成切片未被过滤")
			return 1
		}
	}
	if write {
		fmt.Fprintln(out, "本机Qdrant合成向量写入与班级过滤查询通过；仅验证测试集合和4维数字，不含讲义正文或外部模型调用。")
	} else {
		fmt.Fprintln(out, "本机Qdrant容器重启后，合成向量仍可查询且班级过滤通过。")
	}
	return 0
}

func retryQdrant(ctx context.Context, check func() error) error {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var err error
	for {
		if err = check(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return err
		case <-ticker.C:
		}
	}
}

func serve(ctx context.Context, listener net.Listener, handler http.Handler, out, errOut io.Writer) int {
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Minute, WriteTimeout: 10 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Fprintln(out, "后端已完成数据库、账号及讲义文件核对；服务通过本机检查入口运行。")
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(errOut, "HTTP服务异常停止")
			return 1
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			fmt.Fprintln(errOut, "HTTP服务关闭超时")
			return 1
		}
	}
	return 0
}
