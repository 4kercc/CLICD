UA='Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
for u in \
  "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso" \
  "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/latest-virtio/virtio-win.iso" \
  "https://dl.fedoraproject.org/pub/alt/virtio-win/latest/images/virtio-win.iso" ; do
  echo "--- $u"
  curl -s -A "$UA" -r 0-65535 -L --max-time 40 -o /tmp/probe.bin \
    -w 'http=%{http_code} bytes=%{size_download} type=%{content_type}\n' "$u"
  printf 'head: '; head -c 20 /tmp/probe.bin | tr -d '\000'; echo
done
echo "--- 校验 ISO9660 魔数 (0x8001)"
