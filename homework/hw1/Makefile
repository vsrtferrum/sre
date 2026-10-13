setup: 
	go build -o load/loadgen  load/load.go  
	./setup.sh
clear: 
	kind delete cluster --name hw1

NUMS := 1 2 3
SCENARIOS := $(NUMS:%=scenario%)

.PHONY: all $(SCENARIOS)

all: scenario1

$(SCENARIOS): scenario%: load/scenario%.sh
	$<