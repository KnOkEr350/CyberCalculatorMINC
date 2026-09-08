#!/usr/bin/env python3
"""Read official monitoring links to stdout; never modifies the database.

No arguments: regions of the 2025 monitoring. A numeric region id: universities
and branches listed for that region. This is NOT the licensing register.
"""
import json
import re
import sys
from html.parser import HTMLParser
from urllib.request import Request, urlopen


class Links(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.items = []
        self.href = None
        self.parts = []

    def handle_starttag(self, tag, attrs):
        if tag == "a":
            self.href = dict(attrs).get("href", "")
            self.parts = []

    def handle_data(self, data):
        if self.href is not None:
            self.parts.append(data)

    def handle_endtag(self, tag):
        if tag == "a" and self.href is not None:
            self.items.append((self.href, " ".join("".join(self.parts).split())))
            self.href = None


region = sys.argv[1] if len(sys.argv) > 1 else None
if region and not region.isdigit():
    raise SystemExit("Region id must be numeric")
base = "https://monitoring.miccedu.ru/"
url = (base + "iam/2025/_vpo/material.php?type=2&id=" + region
       if region else base + "?m=vpo&year=2025")
with urlopen(Request(url, headers={"User-Agent": "CyberCalculator-directory-review/1.0"}), timeout=25) as response:
    parser = Links()
    parser.feed(response.read(5_000_001).decode("utf-8"))
pattern = r"inst\.php\?id=(\d+)$" if region else r"iam/2025/_vpo/material\.php\?type=2&id=(\d+)$"
out = {}
for href, name in parser.items:
    match = re.fullmatch(pattern, href)
    if match and name:
        out[match[1]] = {"id": match[1], "name": name}
print(json.dumps(list(out.values()), ensure_ascii=False))
