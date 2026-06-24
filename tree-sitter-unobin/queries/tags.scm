((field
  (value_field
    key: (field_key (identifier) @_block)
    value: (expression
      (primary_expression
        (object
          (field
            (value_field
              key: (field_key (identifier) @name)) @definition.var))))))
  (#match? @_block "^inputs$"))

((field
  (value_field
    key: (field_key (identifier) @_block)
    value: (expression
      (primary_expression
        (object
          (field
            (value_field
              key: (field_key (identifier) @name)) @definition.var))))))
  (#match? @_block "^locals$"))

((field
  (value_field
    key: (field_key (identifier) @_block)
    value: (expression
      (primary_expression
        (object
          (field
            (value_field
              key: (field_key (identifier) @name)) @definition.var))))))
  (#match? @_block "^imports$"))

((field
  (value_field
    key: (field_key (identifier) @_block)
    value: (expression
      (primary_expression
        (object
          (field
            (value_field
              key: (field_key (identifier) @name)) @definition.var))))))
  (#match? @_block "^library-configs$"))

((field
  (value_field
    key: (field_key (identifier) @_block)
    value: (expression
      (primary_expression
        (object
          (field
            (value_field
              key: (field_key (identifier) @name)
              value: (selector_body_value)) @definition.function))))))
  (#match? @_block "^resources$"))

((field
  (value_field
    key: (field_key (identifier) @_block)
    value: (expression
      (primary_expression
        (object
          (field
            (value_field
              key: (field_key (identifier) @name)
              value: (selector_body_value)) @definition.function))))))
  (#match? @_block "^data-sources$"))

((field
  (value_field
    key: (field_key (identifier) @_block)
    value: (expression
      (primary_expression
        (object
          (field
            (value_field
              key: (field_key (identifier) @name)
              value: (selector_body_value)) @definition.function))))))
  (#match? @_block "^actions$"))

((field
  (value_field
    key: (field_key (identifier) @_block)
    value: (expression
      (primary_expression
        (object
          (field
            (value_field
              key: (field_key (identifier) @name)) @definition.var))))))
  (#match? @_block "^outputs$"))

(field
  (value_field
    key: (field_key (identifier) @name)
    value: (selector_body_value)) @definition.function)
