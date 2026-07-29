BINARY := gopher-sage
CMD := ./cmd/gopher-sage
BIN_DIR := bin

LEAKY_SERVER_DIR := fixtures/sources/leaky_server
LEAKY_SERVER_BINARY := leaky_server
LEAKY_SERVER_PID := $(BIN_DIR)/$(LEAKY_SERVER_BINARY).pid
LEAKY_SERVER_LOG := $(BIN_DIR)/$(LEAKY_SERVER_BINARY).log
LEAKY_SERVER_URL := http://localhost:6060/work
TRAFFIC_CONCURRENCY ?= 10

.PHONY: build clean leaky-server-start leaky-server-stop leaky-server-traffic
build: clean
	go build -o $(BIN_DIR)/$(BINARY) $(CMD)
clean:
	rm -f $(BIN_DIR)/$(BINARY)

leaky-server-start:
	@mkdir -p $(BIN_DIR)
	@if [ -f $(LEAKY_SERVER_PID) ] && kill -0 $$(cat $(LEAKY_SERVER_PID)) 2>/dev/null; then \
		echo "leaky_server already running (pid $$(cat $(LEAKY_SERVER_PID)))"; \
	else \
		go build -C $(LEAKY_SERVER_DIR) -o $(abspath $(BIN_DIR))/$(LEAKY_SERVER_BINARY) . || exit 1; \
		$(BIN_DIR)/$(LEAKY_SERVER_BINARY) > $(LEAKY_SERVER_LOG) 2>&1 & echo $$! > $(LEAKY_SERVER_PID); \
		echo "leaky_server started (pid $$(cat $(LEAKY_SERVER_PID))), logs at $(LEAKY_SERVER_LOG)"; \
	fi

leaky-server-stop:
	@if [ -f $(LEAKY_SERVER_PID) ] && kill -0 $$(cat $(LEAKY_SERVER_PID)) 2>/dev/null; then \
		kill $$(cat $(LEAKY_SERVER_PID)) && echo "leaky_server stopped (pid $$(cat $(LEAKY_SERVER_PID)))"; \
		rm -f $(LEAKY_SERVER_PID); \
	else \
		echo "leaky_server is not running"; \
		rm -f $(LEAKY_SERVER_PID); \
	fi

leaky-server-traffic:
	@echo "continuously sending traffic ($(TRAFFIC_CONCURRENCY) concurrent) to $(LEAKY_SERVER_URL) (Ctrl-C to stop)"
	@yes | xargs -P $(TRAFFIC_CONCURRENCY) -I {} curl -s -o /dev/null $(LEAKY_SERVER_URL)
