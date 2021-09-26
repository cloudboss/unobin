# readfile

Read a file.

# Arguments

`src`: (Required, type _string_) - Source file to read. If the source path begins with a `/`, it is treated as an absolute path, otherwise it is relative to the `resources` directory of the playbook and must be compiled in.

# Outputs

`content`: (type _string_) - A base64 encoded string containing the contents of the source file.

# Example

Example usage in a playbook:

```
imports {
  readfile: 'github.com/cloudboss/unobin/modules/file/readfile.ReadFile'
  xyz: 'github.com/cloudboss/unobin/modules/misc/xyz.XYZ'
}

task [read /etc/xyz.conf] {
  module: readfile
  args: {
    src: '/etc/xyz.conf'
  }
}

task [configure xyz] {
  module: xyz
  args: {
    config: b64-decode(string-output[read /etc/xyz.conf].content)
  }
}
```
