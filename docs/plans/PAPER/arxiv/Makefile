LATEXMK := latexmk
TARGET := PAPER_arXiv

.PHONY: all clean check archive

all: $(TARGET).pdf

$(TARGET).pdf: main.tex references.bib figures/qwen-governed-exchange.png \
	figures/governed-exchange-lifecycle.tex figures/model-identity-chain.tex \
	figures/primitive-latency-plot.tex benchmark-data/core-primitives.tsv
	$(LATEXMK) -pdf -interaction=nonstopmode -halt-on-error -file-line-error -jobname=$(TARGET) main.tex

check: $(TARGET).pdf
	@test -s $(TARGET).pdf
	@! grep -E 'Citation .* undefined|Reference .* undefined|There were undefined references|Overfull \\hbox' $(TARGET).log

clean:
	$(LATEXMK) -C -jobname=$(TARGET) main.tex

archive: check
	tar -czf PAPER-arXiv-source.tar.gz main.tex references.bib Makefile README.md figures benchmark-data
