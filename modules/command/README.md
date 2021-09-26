# command

Run a command.

# Arguments

`execute`: (Required, type _string_) - Command with arguments to execute.

`creates`: (Optional, type _string_) - Path to a file or directory whose existence will cause the playbook to skip executing the command. This implies that the file is normally created by the command when run, so its existence means the command has run previously.

`removes` (Optional, type _string_) - Path to a file or directory whose absence will cause the playbook to skip executing the command. This implies that the file is normally removed by the command when run, so its absence means the command has run previously.

# Outputs

`exit_status`: (type _int_) - Exit status of command.

`stdout`: (type _string_) - Standard output of command.

`stderr`: (type _string_) - Standard error of command.

`stdout_lines`: (type _array_ of _string_) - Standard output of command as an array, one string per line.

`stderr_lines`: (type _array_ of _string_) - Standard error of command as an array, one string per line.

# Example

Example usage in a playbook:

```
imports {
  command: 'github.com/cloudboss/unobin/modules/command.Command'
}

task [get usernames from /etc/passwd] {
  module: command
  args: {
    execute: 'cut -d : -f 1 /etc/passwd'
  }
}
```
