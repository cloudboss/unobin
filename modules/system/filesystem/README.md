# filesystem

Ensure a filesystem is present.

# Arguments

`device`: (Required, type _string_) - The path to the device on which to create the filesystem.

`fstype`: (Required, type _string_) - The type of the filesystem.

`force`: (Optional, type _bool_) - Attempt to force filesystem to be created. This can still fail, for example if a filesystem is present on the device and it is mounted.

`opts`: (Optional, type _list_ of _string_) - An array of options to pass when creating the filesystem.

# Outputs

`device`: (type _string_) - Device containing filesystem.

`fstype`: (type _string_) - Filesystem type.

`stdout`: (type _string_) - Standard output of filesytem creation command.

`stderr`: (type _string_) - Standard error of filesytem creation command.

`stdout_lines`: (type _array_ of _string_) - Standard output of filesytem creation command as an array, one string per line.

`stderr_lines`: (type _array_ of _string_) - Standard error of filesytem creation command as an array, one string per line.

# Example

Example usage in a playbook:

```
imports {
  filesystem: 'github.com/cloudboss/unobin/modules/system/filesystem.Filesystem'
}

task [ensure an ext4 filesystem is present on the home partition] {
  module: filesystem
  args: {
    device: '/dev/nvme0n1p3'
    fstype: 'ext4'
    opts: [
      '-b' '2048'
      '-d' '/xyz'
    ]
  }
}
```
