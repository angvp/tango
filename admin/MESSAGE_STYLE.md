# Admin message style

Every message the admin shows to a user follows these rules. Review checks each new message against them.

## Rules

- Say what happened, then what to do. Short sentences.
- A message beside its field does not name the field, because the field's own label is beside it. The form summary puts the label in front of the message ("Copies: Enter a whole number."), so the label is never repeated. Never name a field by its Go name or in code font.
- Name rows with the model's display nouns (`admin.Options` `SingularName` and `PluralName`). A model without them uses its humanized Go name.
- Use "you" only to give an instruction. State facts without apology.
- Use the same verb for the same action everywhere: Save, Delete, Cancel. A confirm button names its action ("Delete 3 books"), never "OK".
- Counts use the words the list uses: "3 books deleted", "1 book deleted", not "rows".
- Never show Go error text, SQL, a decoder message, a stack trace or a quoted document. The detail goes to the server log.
- Avoid technical terms: "constraint", "null", "int64", "CSRF", "token", "panic", "decoder". Describe the result instead: "This code is already used by another book."

## Keys

- Every built-in message is an `admin.*` key with an English fallback. A host overrides one key through its own catalog.
- The key name and its placeholders are part of the compatibility contract. The English wording may change in any release.
- A catalog string must keep the fallback's format verbs, as the i18n package compares them: the sequence of (argument, verb) pairs must match the fallback's. A string whose pairs differ is ignored and the English fallback is used.

## Known limit

Messages a widget produces itself stay English until the widget interface receives a request context.
