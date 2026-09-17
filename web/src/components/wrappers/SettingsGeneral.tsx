import { Fragment, useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSeparator,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { PageHeader } from '@/components/wrappers/PageHeader'
import { readRefusal } from '@/lib/api'
import { wordList } from '@/lib/format'
import { useUnsavedChanges } from '@/lib/navigation-guard'
import {
  fromInput,
  groupTitle,
  problemsByKey,
  settingGroups,
  settingLabel,
  sourceLabel,
  toInput,
  type Setting,
  type SettingInput,
  type SettingsResponse,
} from '@/lib/settings'

const NO_PROBLEMS = { byKey: {} as Record<string, string>, general: [] as string[] }

/**
 * The machine-wide settings in `config.yaml`, grouped by what they govern.
 * Nothing is written until Save: changes and resets collect as a draft, and
 * one save sends them together. A refused save writes nothing and names each
 * problem beside its setting.
 *
 * Settings the web may not change, the daemon's address and where vault text
 * is sent for embedding, are shown but locked, with where to change them.
 */
export function SettingsGeneral() {
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [drafts, setDrafts] = useState<Record<string, SettingInput>>({})
  const [unset, setUnset] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [problems, setProblems] = useState(NO_PROBLEMS)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await fetch('/api/settings', { signal })
      if (!response.ok) throw new Error((await readRefusal(response)).message)
      setData((await response.json()) as SettingsResponse)
      setError(null)
    } catch (err) {
      if (signal?.aborted) return
      setError(err instanceof Error ? err.message : 'Could not load the settings')
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const settings = useMemo(() => data?.settings ?? [], [data])
  const groups = useMemo(() => settingGroups(settings), [settings])
  const changed = settings.filter(
    (setting) => setting.key in drafts && drafts[setting.key] !== toInput(setting.type, setting.value),
  )
  const dirty = changed.length > 0 || unset.length > 0
  useUnsavedChanges(dirty)

  const change = (key: string, input: SettingInput) => {
    setDrafts({ ...drafts, [key]: input })
    // Typing a value takes back a pending reset.
    setUnset(unset.filter((item) => item !== key))
    if (problems.byKey[key]) setProblems({ ...problems, byKey: { ...problems.byKey, [key]: '' } })
  }

  const reset = (key: string) => {
    const rest = { ...drafts }
    delete rest[key]
    setDrafts(rest)
    setUnset([...unset, key])
  }

  const save = async (event: FormEvent) => {
    event.preventDefault()
    if (!dirty || saving) return
    setSaving(true)
    try {
      const response = await fetch('/api/settings', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          set: Object.fromEntries(changed.map((setting) => [setting.key, fromInput(setting.type, drafts[setting.key])])),
          unset,
        }),
      })
      if (!response.ok) {
        const refusal = await readRefusal(response)
        setProblems(
          refusal.problems.length
            ? problemsByKey(refusal.problems, settings.map((setting) => setting.key))
            : { byKey: {}, general: [refusal.message] },
        )
        return
      }
      const saved = (await response.json()) as SettingsResponse
      setData(saved)
      setDrafts({})
      setUnset([])
      setProblems(NO_PROBLEMS)
      toast.add({ title: 'Settings saved', type: 'success' })
      if (saved.restart?.length) {
        toast.add({
          title: 'Restart the daemon to apply',
          description: `${wordList(saved.restart)} ${saved.restart.length === 1 ? 'applies' : 'apply'} after trellis daemon restart.`,
          type: 'warning',
        })
      }
    } catch (err) {
      setProblems({ byKey: {}, general: [err instanceof Error ? err.message : 'Could not reach the daemon.'] })
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <PageHeader
        title="General"
        description={data ? `Settings for every project on this machine, kept in ${data.file}.` : 'Settings for every project on this machine.'}
        actions={
          <Button type="submit" size="sm" form="settings-form" disabled={!dirty || saving}>
            {saving && <Spinner data-icon="inline-start" />}
            Save
          </Button>
        }
      />

      {error && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Could not load the settings</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {problems.general.length > 0 && (
        <Alert variant="destructive" className="mt-6">
          <AlertTitle>Nothing was saved</AlertTitle>
          <AlertDescription>
            <ul className="ml-4 list-disc">
              {problems.general.map((problem) => <li key={problem}>{problem}</li>)}
            </ul>
          </AlertDescription>
        </Alert>
      )}

      {!data && !error && (
        <div className="mt-6 flex flex-col gap-6">
          {[0, 1, 2, 3].map((row) => <Skeleton key={row} className="h-14 w-full" />)}
        </div>
      )}

      {data && (
        // The server checks every value and names each problem beside its
        // setting; the browser's own bubbles would speak in another voice.
        <form id="settings-form" noValidate onSubmit={save} className="mt-6">
          <FieldGroup>
            {groups.map((group, index) => (
              <Fragment key={group.name}>
                {index > 0 && <FieldSeparator />}
                <FieldSet>
                  <FieldLegend>{groupTitle(group.name)}</FieldLegend>
                  <FieldGroup>
                    {group.settings.map((setting) => (
                      <SettingRow
                        key={setting.key}
                        setting={setting}
                        input={
                          setting.key in drafts
                            ? drafts[setting.key]
                            : toInput(setting.type, unset.includes(setting.key) ? setting.default : setting.value)
                        }
                        resetting={unset.includes(setting.key)}
                        problem={problems.byKey[setting.key]}
                        onChange={(input) => change(setting.key, input)}
                        onReset={() => reset(setting.key)}
                        onUndoReset={() => setUnset(unset.filter((item) => item !== setting.key))}
                      />
                    ))}
                  </FieldGroup>
                </FieldSet>
              </Fragment>
            ))}
          </FieldGroup>
        </form>
      )}
    </>
  )
}

/**
 * One setting: what it is called and does, its key and where its value comes
 * from on the left, and its control on the right. A value from `config.yaml`
 * can be reset to the default, which applies on save.
 */
function SettingRow({
  setting,
  input,
  resetting,
  problem,
  onChange,
  onReset,
  onUndoReset,
}: {
  setting: Setting
  input: SettingInput
  resetting: boolean
  problem?: string
  onChange: (input: SettingInput) => void
  onReset: () => void
  onUndoReset: () => void
}) {
  const id = `setting-${setting.key}`
  const invalid = Boolean(problem)

  return (
    <Field orientation="responsive" data-invalid={invalid || undefined} data-disabled={!setting.editable || undefined}>
      <FieldContent>
        <FieldLabel htmlFor={id}>{settingLabel(setting.key)}</FieldLabel>
        {setting.description && <FieldDescription>{setting.description}</FieldDescription>}
        <div className="flex min-h-6 flex-wrap items-center gap-x-3 text-xs text-muted-foreground">
          <span className="text-meta">{setting.key}</span>
          {!setting.editable ? (
            <span>Set in config.yaml</span>
          ) : resetting ? (
            <>
              <span>Default after saving</span>
              <Button type="button" variant="ghost" size="xs" onClick={onUndoReset}>Undo</Button>
            </>
          ) : (
            <>
              <span>{sourceLabel(setting)}</span>
              {setting.source === 'config' && (
                <Button type="button" variant="ghost" size="xs" onClick={onReset}>Reset</Button>
              )}
            </>
          )}
          {setting.restart && <span>Applies after a restart</span>}
        </div>
        {problem && <FieldError>{problem}</FieldError>}
      </FieldContent>
      {/* The field sizes its direct children, so the control's width is set one level in. */}
      <div className="shrink-0">
        <div className="flex w-full justify-end @md/field-group:w-56">
          <SettingControl id={id} setting={setting} input={input} invalid={invalid} onChange={onChange} />
        </div>
      </div>
    </Field>
  )
}

/** The control a setting's type calls for. */
function SettingControl({
  id,
  setting,
  input,
  invalid,
  onChange,
}: {
  id: string
  setting: Setting
  input: SettingInput
  invalid: boolean
  onChange: (input: SettingInput) => void
}) {
  const disabled = !setting.editable
  const aria = invalid || undefined

  switch (setting.type) {
    case 'bool':
      return (
        <Switch
          id={id}
          checked={Boolean(input)}
          disabled={disabled}
          aria-invalid={aria}
          onCheckedChange={(checked) => onChange(checked)}
        />
      )
    case 'enum': {
      const choices = setting.choices ?? []
      return (
        <Select
          items={choices.map((choice) => ({ value: choice, label: choice }))}
          value={String(input)}
          disabled={disabled}
          onValueChange={(value) => { if (value !== null) onChange(value) }}
        >
          <SelectTrigger id={id} className="w-full" aria-invalid={aria}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {choices.map((choice) => <SelectItem key={choice} value={choice}>{choice}</SelectItem>)}
          </SelectContent>
        </Select>
      )
    }
    case 'int':
    case 'number':
      return (
        <Input
          id={id}
          type="number"
          inputMode={setting.type === 'int' ? 'numeric' : 'decimal'}
          step={setting.type === 'int' ? 1 : 'any'}
          min={setting.min}
          disabled={disabled}
          aria-invalid={aria}
          value={String(input)}
          onChange={(event) => onChange(event.target.value)}
        />
      )
    default:
      return (
        <Input
          id={id}
          autoComplete="off"
          spellCheck={false}
          placeholder={setting.type === 'duration' ? '30m' : setting.type === 'list' ? 'one, two, three' : undefined}
          disabled={disabled}
          aria-invalid={aria}
          value={String(input)}
          onChange={(event) => onChange(event.target.value)}
        />
      )
  }
}
