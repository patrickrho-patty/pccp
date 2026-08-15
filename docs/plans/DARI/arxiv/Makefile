LATEXMK := latexmk
EN_TARGET := DARI_arXiv
KO_TARGET := DARI_arXiv_KO
ARCHIVE := DARI-arXiv-source.tar.gz
FIG_TEX := figures/governed-exchange-lifecycle.tex figures/governed-exchange-lifecycle-ko.tex figures/model-identity-chain.tex figures/model-identity-chain-ko.tex figures/primitive-latency-plot.tex figures/primitive-latency-plot-ko.tex
BENCH_TSV := benchmark-data/core-primitives.tsv
COMMON_DEPS := references.bib $(FIG_TEX) $(BENCH_TSV) $(wildcard figures/*.png)

.PHONY: all verify check clean archive

all: $(EN_TARGET).pdf $(KO_TARGET).pdf

$(EN_TARGET).pdf: main.tex $(COMMON_DEPS)
	$(LATEXMK) -pdf -interaction=nonstopmode -halt-on-error -file-line-error -jobname=$(EN_TARGET) main.tex

$(KO_TARGET).pdf: main_ko.tex $(COMMON_DEPS)
	$(LATEXMK) -xelatex -interaction=nonstopmode -halt-on-error -file-line-error -jobname=$(KO_TARGET) main_ko.tex

verify: all
	@test -s $(EN_TARGET).pdf
	@test -s $(KO_TARGET).pdf
	@test -s $(EN_TARGET).log
	@test -s $(KO_TARGET).log
	@! grep -E 'Undefined control sequence|Citation .* undefined|Reference .* undefined|There were undefined references|Overfull \\hbox' $(EN_TARGET).log $(KO_TARGET).log

check: verify

clean:
	$(LATEXMK) -C -jobname=$(EN_TARGET) main.tex
	$(LATEXMK) -C -xelatex -jobname=$(KO_TARGET) main_ko.tex

archive: verify
	@tmp_tar=$$(mktemp $(ARCHIVE).tar.XXXXXX); \
	tmp_archive=$$(mktemp $(ARCHIVE).gz.XXXXXX); \
	trap 'rm -f "$$tmp_tar" "$$tmp_archive"' 0 1 2 3 15; \
	COPYFILE_DISABLE=1 tar -cf "$$tmp_tar" main.tex main_ko.tex references.bib Makefile README.md implementation-claim-matrix.tsv figures benchmark-data && \
	gzip -n -c "$$tmp_tar" > "$$tmp_archive" && \
	mv "$$tmp_archive" $(ARCHIVE)
