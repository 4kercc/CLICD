UA='Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36'
echo "--- A) dl.fedoraproject 目录列表"
curl -s -A "$UA" -L --max-time 25 "https://dl.fedoraproject.org/pub/alt/virtio-win/" -w '\n[http=%{http_code}]\n' | head -20
echo "--- B) 目录列表 2"
curl -s -A "$UA" -L --max-time 25 "https://dl.fedoraproject.org/pub/alt/virtio-win/stable-virtio/" -w '\n[http=%{http_code}]\n' | head -20
