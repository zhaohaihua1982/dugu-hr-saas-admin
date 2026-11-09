OUTPUT := main
BINARY := dugu-hr-saas-admin

.PHONY: build run test fmt vet clean start stop restart status

# 后台服务管理（更适合日常联调，日志按天写入 logs/）
start:   ## 后台启动（幂等；SKIP_BUILD=1 可跳过编译）
	bash scripts/start.sh

stop:    ## 优雅停止（SIGTERM，超时强杀兜底）
	bash scripts/stop.sh

restart: ## 重启
	bash scripts/restart.sh

status:  ## 查看服务状态
	@if [ -f logs/saas-admin.pid ] && kill -0 "$$(cat logs/saas-admin.pid)" 2>/dev/null; then \
		echo "✅ 运行中：pid=$$(cat logs/saas-admin.pid)"; \
		curl -s -m 2 http://127.0.0.1:8080/healthz && echo; \
	else \
		echo "⛔ 未运行"; \
	fi

.PHONY: build run test fmt vet clean

build:
	@echo "build project to ./$(OUTPUT)"
	go build -o ./$(OUTPUT) ./cmd

run:
	@echo "start $(BINARY) ..."
	go build -o /tmp/$(BINARY) ./cmd && /tmp/$(BINARY)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -f $(OUTPUT)
