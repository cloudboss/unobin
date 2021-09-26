# copy

Copy a file.

# Arguments

`dest`: (Required, type _string_) - Destination file.

`src`: (Conditional, type _string_) - Source file to copy to `dest`. One of `src` or `content` must be defined.

`content`: (Conditional, type _string_) - Content to copy to `dest`. One of `src` or `content` must be defined.

`mode`: (Optional, type _string_, default `0644`) - Mode of destination file.

`owner`: (Optional, type _string_) - Owner of destination file, defaults to the user running the playbook.

`group`: (Optional, type _string_) - Group of destination file, defaults to the group of the user running the playbook.

# Outputs

This module has no outputs.

# Example

Example usage in a playbook:

```
imports {
  copy: 'github.com/cloudboss/unobin/modules/file/copy.Copy'
}

task [copy a file] {
  module: copy
  args: {
    src: '/xyz/abc'
    dest: '/cba/zyx'
    mode: '0400'
  }
}
```
