#!/bin/sh
set -e
if ! getent group skilltree >/dev/null; then
  groupadd --system skilltree
fi
if ! getent passwd skilltree >/dev/null; then
  useradd --system --gid skilltree --home-dir /var/lib/skilltree --shell /usr/sbin/nologin skilltree
fi
mkdir -p /var/lib/skilltree /etc/skilltree
chown skilltree:skilltree /var/lib/skilltree
chmod 0750 /etc/skilltree
# nginx package may not be installed yet; ensure dirs exist for our packaged files
mkdir -p /etc/nginx/conf.d /etc/nginx/snippets /etc/nginx/sites-available
systemctl daemon-reload || true
systemctl enable skilltree.service || true
echo "Skilltree installed. Edit /etc/skilltree/skilltree.env then: systemctl start skilltree"
if [ -d /etc/nginx ]; then
  echo "nginx:"
  echo "  - upstream/zones: /etc/nginx/conf.d/skilltree-upstream.conf (active in http {})"
  echo "  - snippets:       /etc/nginx/snippets/skilltree-*.conf"
  echo "  - site:           /etc/nginx/sites-available/skilltree (not enabled)"
  echo "  Enable after editing server_name + TLS paths:"
  echo "    ln -s /etc/nginx/sites-available/skilltree /etc/nginx/sites-enabled/skilltree"
  echo "    nginx -t && systemctl reload nginx"
fi
