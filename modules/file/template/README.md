# template

Expand a template file and copy it to a destination. Templates files must be in Go [`html/template`](https://golang.org/pkg/html/template/) format.

# Arguments

`src`: (Required, type _string_) - Go template source file. If the source path begins with a `/`, it is treated as an absolute path, otherwise it is relative to the `resources` directory of the playbook and must be compiled in.

`dest`: (Required, type _string_) - Destination file.

`mode`: (Optional, type _string_, default `0644`) - Mode of destination file.

`owner`: (Optional, type _string_) - Owner of destination file, defaults to the user running the playbook.

`group`: (Optional, type _string_) - Group of destination file, defaults to the group of the user running the playbook.

`vars`: (Optional, type _object_ of _string_ -> _any_) - Variables to use for template expansion.

# Outputs

This module has no outputs.

# Example

Example usage in a playbook:

```
imports {
  template: 'github.com/cloudboss/unobin/modules/file/template.Template'
}

task [expand a template] {
  module: template
  args: {
    src: 'etc/xyz.conf.template'
    dest: '/etc/xyz.conf'
    vars: {
      'count': 123
      'verbose': false
      'log-file': '/var/log/xyz.log'
    }
  }
}
```
