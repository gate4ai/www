#!/bin/sh
# Рендер картинок превью ссылок (Open Graph) из SVG: static/og/og-<lang>.svg → static/og-<lang>.png, 1200×630.
# Нужны Chrome (google-chrome / chromium), шрифт Noto Sans с кириллицей и python3 с Pillow, собранным с
# libimagequant (ужимает PNG до 256 цветов без потери акцентов: ~95 КБ вместо ~260). Запуск: sh static/og/render.sh
set -eu
cd "$(dirname "$0")"
chrome=$(command -v google-chrome || command -v google-chrome-stable || command -v chromium || command -v chromium-browser)
for lang in en ru; do
  out="$PWD/../og-$lang.png"
  "$chrome" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 \
    --window-size=1200,630 --screenshot="$out" "file://$PWD/og-$lang.svg" 2>/dev/null
  python3 -c "
import sys; from PIL import Image
im = Image.open(sys.argv[1]).convert('RGB').quantize(256, method=Image.Quantize.LIBIMAGEQUANT)
im.save(sys.argv[1], optimize=True)" "$out"
  echo "og-$lang.png: $(wc -c < "$out") bytes"
done
