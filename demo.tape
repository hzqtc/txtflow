Output demo.gif

Sleep 500ms

Type "cat formula.json | txtflow"
Enter
Sleep 500ms

Type "grep firefox"
Enter
Sleep 1500ms

Type " | head -n 1"
Enter
Sleep 1500ms

Type " | sed 's/,$//'"
Enter
Sleep 1500ms

Type " | jq -r '.homepage'"
Enter
Sleep 1500ms

Ctrl+X
Sleep 2s
