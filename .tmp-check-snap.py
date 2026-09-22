import sqlite3

conn = sqlite3.connect('file:/root/.clicd/config.db?mode=ro', uri=True)
cur = conn.cursor()
cur.execute("SELECT sql FROM sqlite_master WHERE name='snapshots'")
for r in cur.fetchall():
    print(r[0])
cur.execute('PRAGMA table_info(snapshots)')
print('columns:', [c[1] for c in cur.fetchall()])
cur.execute('SELECT count(*) FROM snapshots')
print('snapshot rows:', cur.fetchone()[0])
cur.execute("SELECT id, description FROM snapshots ORDER BY created_at DESC LIMIT 5")
for r in cur.fetchall():
    print('row:', r)
