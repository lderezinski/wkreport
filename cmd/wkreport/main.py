import html
from datetime import datetime

data = [
    ("KEY", "SUMMARY", "STATUS", "PARENT", "RESOLVED"),
    ("ABC-1", "Something", "In Progress", "ABC", datetime.now().strftime("%Y-%m-%d %H:%M")),
]

table = '<table>\n'
for row in data:
    table += '  <tr>' + ''.join(f'<td>{html.escape(cell)}</td>' for cell in row) + '</tr>\n'
table += '</table>'
print(table)
