"""Draw the diagrams and compile the report PDF (the course's LaTeX template).

The report is docs/D7065E_Group9_Final_Report.tex. Each diagram is a Mermaid text file in docs/diagrams/
and is drawn as a PNG in docs/figures/ (diagrams/container.mmd -> figures/container.png):

  docs/diagrams/<name>.mmd  ->  docs/figures/<name>.png
  docs/D7065E_Group9_Final_Report.tex  ->  docs/D7065E_Group9_Final_Report.pdf

Needs: mermaid-cli (`mmdc`, or npx downloads it) and pdflatex. Without a local pdflatex it
uses Docker (TEX_IMAGE, default texlive/texlive:latest-medium).

    python3 scripts/build_report.py                  # diagrams + PDF
    python3 scripts/build_report.py --diagrams-only  # after editing a .mmd file
"""
import os
import shlex
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs"
DIAGRAMS = DOCS / "diagrams"  # Mermaid sources, one file per diagram
FIGURES = DOCS / "figures"    # every picture in the report
REPORT = "D7065E_Group9_Final_Report"  # docs/<REPORT>.tex is the source, docs/<REPORT>.pdf the result
DIAGRAM_NAMES = ["context", "container", "component", "pipeline", "dynamic", "state", "deployment"]
MMDC = os.environ.get("MMDC", "npx -y @mermaid-js/mermaid-cli@11.4.2")
TEX_IMAGE = os.environ.get("TEX_IMAGE", "texlive/texlive:latest-medium")
TEX_EXTRA = "titlesec xurl newunicodechar pgfplots"  # not in the medium TeX Live scheme


def run(cmd, **kw):
    print("+", cmd if isinstance(cmd, str) else " ".join(cmd))
    subprocess.run(cmd, check=True, **kw)


def draw_diagrams():
    """Draw every diagrams/<name>.mmd as figures/<name>.png (3x resolution, sharp in print).
    The colours are set by the %%{init} line at the top of each .mmd file, so the PNG looks the
    same as the diagram on GitHub or in an editor preview."""
    puppeteer = os.environ.get("PUPPETEER_CONFIG")
    for name in DIAGRAM_NAMES:
        src, png = DIAGRAMS / f"{name}.mmd", FIGURES / f"{name}.png"
        cmd = shlex.split(MMDC) + ["-i", str(src), "-o", str(png), "-s", "3", "-b", "white"]
        if puppeteer:
            cmd += ["-p", puppeteer]
        run(cmd, stdout=subprocess.DEVNULL)


def compile_pdf():
    """pdflatex twice (the second run fills in the table of contents), then remove the build files."""
    tex = f"pdflatex -interaction=nonstopmode -halt-on-error {REPORT}.tex"
    if shutil.which("pdflatex"):
        for _ in range(2):
            run(tex, shell=True, cwd=DOCS, stdout=subprocess.DEVNULL)
    else:
        script = f"(tlmgr install {TEX_EXTRA} >/dev/null 2>&1 || true); {tex} >/dev/null && {tex} >/dev/null"
        run(["docker", "run", "--rm", "-v", f"{DOCS}:/work", "-w", "/work", TEX_IMAGE, "sh", "-c", script])
    for ext in ("aux", "log", "out", "toc"):
        (DOCS / f"{REPORT}.{ext}").unlink(missing_ok=True)


def main():
    draw_diagrams()
    if "--diagrams-only" in sys.argv:
        print(f"drew {len(DIAGRAM_NAMES)} diagrams in docs/figures/")
        return
    compile_pdf()
    print(f"wrote docs/{REPORT}.pdf")


if __name__ == "__main__":
    main()
