"""Build the complete product manuals with the established guide styles.

Run using the Codex bundled Python runtime. Render and inspect all pages with
the Documents skill before releasing PDFs or updating the distribution ZIP.
"""
from pathlib import Path
from build_guides import build

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
GUIDES = [
    ('01-product-walkthrough', 'CESERVE_01_Complete_Product_Walkthrough', 'Complete product walkthrough'),
    ('02-configuration-administration', 'CESERVE_02_Configuration_and_Administration', 'Configuration and administration'),
    ('03-operations-finance-reporting', 'CESERVE_03_Operations_Finance_and_Reporting', 'Operations finance and reporting'),
    ('04-questions-support', 'CESERVE_04_Questions_and_Support', 'Questions and support'),
]

if __name__ == '__main__':
    dest = ROOT / 'output' / 'complete-product' / 'docx'
    dest.mkdir(parents=True, exist_ok=True)
    for i, (source, filename, short) in enumerate(GUIDES, 1):
        build(HERE / 'complete' / (source + '.md'), dest / (filename + '.docx'), short, i)
