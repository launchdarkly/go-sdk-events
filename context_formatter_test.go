package ldevents

import (
	"sort"
	"testing"

	"gopkg.in/launchdarkly/go-sdk-common.v3/ldcontext"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldvalue"

	m "github.com/launchdarkly/go-test-helpers/v2/matchers"
	"gopkg.in/launchdarkly/go-jsonstream.v1/jwriter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventContextFormatterConstructor(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		f := newEventContextFormatter(EventsConfiguration{})
		require.NotNil(t, f)

		assert.False(t, f.allAttributesPrivate)
		assert.Nil(t, f.privateAttributes)
	})

	t.Run("all private", func(t *testing.T) {
		f := newEventContextFormatter(EventsConfiguration{
			AllAttributesPrivate: true,
		})
		require.NotNil(t, f)

		assert.True(t, f.allAttributesPrivate)
		assert.Nil(t, f.privateAttributes)
	})

	t.Run("top-level private", func(t *testing.T) {
		private1, private2 := ldcontext.NewAttrRef("name"), ldcontext.NewAttrRef("email")
		f := newEventContextFormatter(EventsConfiguration{
			PrivateAttributes: []ldcontext.AttrRef{private1, private2},
		})
		require.NotNil(t, f)

		assert.False(t, f.allAttributesPrivate)
		require.NotNil(t, f.privateAttributes)
		assert.Equal(t,
			map[string]*privateAttrLookupNode{
				"name":  {attribute: &private1},
				"email": {attribute: &private2},
			},
			f.privateAttributes)
	})

	t.Run("nested private", func(t *testing.T) {
		private1, private2, private3 := ldcontext.NewAttrRef("/name"),
			ldcontext.NewAttrRef("/address/street"), ldcontext.NewAttrRef("/address/city")
		f := newEventContextFormatter(EventsConfiguration{
			PrivateAttributes: []ldcontext.AttrRef{private1, private2, private3},
		})
		require.NotNil(t, f)

		assert.False(t, f.allAttributesPrivate)
		require.NotNil(t, f.privateAttributes)
		assert.Equal(t,
			map[string]*privateAttrLookupNode{
				"name": {attribute: &private1},
				"address": {
					children: map[string]*privateAttrLookupNode{
						"street": {attribute: &private2},
						"city":   {attribute: &private3},
					},
				},
			},
			f.privateAttributes)
	})
}

func TestCheckGlobalPrivateAttrRefs(t *testing.T) {
	expectResult := func(t *testing.T, f *eventContextFormatter, expectRedactedAttr *ldcontext.AttrRef, expectHasNested bool, path ...string) {
		redactedAttr, hasNested := f.checkGlobalPrivateAttrRefs(path)
		assert.Equal(t, expectRedactedAttr, redactedAttr)
		assert.Equal(t, expectHasNested, hasNested)
	}

	t.Run("empty", func(t *testing.T) {
		f := newEventContextFormatter(EventsConfiguration{})
		require.NotNil(t, f)

		expectResult(t, f, nil, false, "name")
		expectResult(t, f, nil, false, "address", "street")
	})

	t.Run("top-level private", func(t *testing.T) {
		attrRef1, attrRef2 := ldcontext.NewAttrRef("name"), ldcontext.NewAttrRef("email")
		f := newEventContextFormatter(EventsConfiguration{
			PrivateAttributes: []ldcontext.AttrRef{attrRef1, attrRef2},
		})
		require.NotNil(t, f)

		expectResult(t, f, &attrRef1, false, "name")
		expectResult(t, f, &attrRef2, false, "email")
		expectResult(t, f, nil, false, "address")
		expectResult(t, f, nil, false, "address", "street")
	})

	t.Run("nested private", func(t *testing.T) {
		attrRef1, attrRef2, attrRef3 := ldcontext.NewAttrRef("name"),
			ldcontext.NewAttrRef("/address/street"), ldcontext.NewAttrRef("/address/city")
		f := newEventContextFormatter(EventsConfiguration{
			PrivateAttributes: []ldcontext.AttrRef{attrRef1, attrRef2, attrRef3},
		})
		require.NotNil(t, f)

		expectResult(t, f, &attrRef1, false, "name")
		expectResult(t, f, nil, true, "address") // note "true" indicating there are nested properties to filter
		expectResult(t, f, &attrRef2, false, "address", "street")
		expectResult(t, f, &attrRef3, false, "address", "city")
		expectResult(t, f, nil, false, "address", "zip")
	})
}

func TestEventContextFormatterOutput(t *testing.T) {
	objectValue := ldvalue.ObjectBuild().Set("city", ldvalue.String("SF")).Set("state", ldvalue.String("CA")).Build()

	type params struct {
		desc         string
		context      ldcontext.Context
		options      EventsConfiguration
		expectedJSON string
	}
	for _, p := range []params{
		{
			"no attributes private, single kind",
			ldcontext.NewBuilder("my-key").Kind("org").
				Name("my-name").
				SetString("attr1", "value1").
				SetValue("address", objectValue).
				Build(),
			EventsConfiguration{},
			`{"kind": "org", "key": "my-key",
				"name": "my-name", "attr1": "value1", "address": {"city": "SF", "state": "CA"}}`,
		},
		{
			"no attributes private, multi-kind",
			ldcontext.NewMulti(
				ldcontext.NewBuilder("org-key").Kind("org").
					Name("org-name").
					Build(),
				ldcontext.NewBuilder("user-key").
					Name("user-name").
					SetValue("address", objectValue).
					Build(),
			),
			EventsConfiguration{},
			`{"kind": "multi",
			    "org": {"key": "org-key", "name": "org-name"},
				"user": {"key": "user-key", "name": "user-name", "address": {"city": "SF", "state": "CA"}}}`,
		},
		{
			"transient",
			ldcontext.NewBuilder("my-key").Kind("org").
				Transient(true).
				Build(),
			EventsConfiguration{},
			`{"kind": "org", "key": "my-key", "transient": true}`,
		},
		{
			"secondary",
			ldcontext.NewBuilder("my-key").Kind("org").
				Secondary("x").
				Build(),
			EventsConfiguration{},
			`{"kind": "org", "key": "my-key", "_meta": {"secondary": "x"}}`,
		},
		{
			"all attributes private globally, single kind",
			ldcontext.NewBuilder("my-key").Kind("org").
				Name("my-name").
				SetString("attr1", "value1").
				SetValue("address", objectValue).
				Build(),
			EventsConfiguration{AllAttributesPrivate: true},
			`{"kind": "org", "key": "my-key",
				"_meta": {"redactedAttributes": ["address", "attr1", "name"]}}`,
		},
		{
			"all attributes private globally, multi-kind",
			ldcontext.NewMulti(
				ldcontext.NewBuilder("org-key").Kind("org").
					Name("org-name").
					Build(),
				ldcontext.NewBuilder("user-key").
					Name("user-name").
					SetValue("address", objectValue).
					Build(),
			),
			EventsConfiguration{AllAttributesPrivate: true},
			`{"kind": "multi",
			    "org": {"key": "org-key", "_meta": {"redactedAttributes": ["name"]}},
				"user": {"key": "user-key", "_meta": {"redactedAttributes": ["address", "name"]}}}`,
		},
		{
			"top-level attributes private globally, single kind",
			ldcontext.NewBuilder("my-key").Kind("org").
				Name("my-name").
				SetString("attr1", "value1").
				SetValue("address", objectValue).
				Build(),
			EventsConfiguration{PrivateAttributes: []ldcontext.AttrRef{
				ldcontext.NewAttrRef("/name"), ldcontext.NewAttrRef("/address")}},
			`{"kind": "org", "key": "my-key", "attr1": "value1",
				"_meta": {"redactedAttributes": ["/address", "/name"]}}`,
		},
		{
			"top-level attributes private globally, multi-kind",
			ldcontext.NewMulti(
				ldcontext.NewBuilder("org-key").Kind("org").
					Name("org-name").
					SetString("attr1", "value1").
					SetString("attr2", "value2").
					Build(),
				ldcontext.NewBuilder("user-key").
					Name("user-name").
					SetString("attr1", "value1").
					SetString("attr3", "value3").
					Build(),
			),
			EventsConfiguration{PrivateAttributes: []ldcontext.AttrRef{
				ldcontext.NewAttrRef("/name"), ldcontext.NewAttrRef("/attr1"), ldcontext.NewAttrRef("/attr3")}},
			`{"kind": "multi",
			    "org": {"key": "org-key", "attr2": "value2", "_meta": {"redactedAttributes": ["/attr1", "/name"]}},
				"user": {"key": "user-key", "_meta": {"redactedAttributes": ["/attr1", "/attr3", "/name"]}}}`,
		},
		{
			"top-level attributes private per context, single kind",
			ldcontext.NewBuilder("my-key").Kind("org").
				Name("my-name").
				SetString("attr1", "value1").
				SetValue("address", objectValue).
				Private("name", "address").
				Build(),
			EventsConfiguration{},
			`{"kind": "org", "key": "my-key", "attr1": "value1",
				"_meta": {"redactedAttributes": ["address", "name"]}}`,
		},
		{
			"top-level attributes private per context, multi-kind",
			ldcontext.NewMulti(
				ldcontext.NewBuilder("org-key").Kind("org").
					SetString("attr1", "value1").
					SetString("attr2", "value2").
					Private("attr1").
					Build(),
				ldcontext.NewBuilder("user-key").
					SetString("attr1", "value1").
					SetString("attr3", "value3").
					Private("attr3").
					Build(),
			),
			EventsConfiguration{},
			`{"kind": "multi",
			    "org": {"key": "org-key", "attr2": "value2", "_meta": {"redactedAttributes": ["attr1"]}},
				"user": {"key": "user-key", "attr1": "value1", "_meta": {"redactedAttributes": ["attr3"]}}}`,
		},
		{
			"nested attribute private globally",
			ldcontext.NewBuilder("my-key").Kind("org").
				Name("my-name").
				SetValue("address", objectValue).
				Build(),
			EventsConfiguration{PrivateAttributes: []ldcontext.AttrRef{ldcontext.NewAttrRef("/address/city")}},
			`{"kind": "org", "key": "my-key",
				"name": "my-name", "address": {"state": "CA"},
				"_meta": {"redactedAttributes": ["/address/city"]}}`,
		},
		{
			"nested attribute private per context",
			ldcontext.NewBuilder("my-key").Kind("org").
				Name("my-name").
				SetValue("address", objectValue).
				PrivateRef(ldcontext.NewAttrRef("/address/city"), ldcontext.NewAttrRef("/name")).
				Build(),
			EventsConfiguration{},
			`{"kind": "org", "key": "my-key", "address": {"state": "CA"},
				"_meta": {"redactedAttributes": ["/address/city", "/name"]}}`,
		},
		{
			"nested attribute private per context, superseded by top-level reference",
			ldcontext.NewBuilder("my-key").Kind("org").
				Name("my-name").
				SetValue("address", objectValue).
				PrivateRef(ldcontext.NewAttrRef("/address/city"), ldcontext.NewAttrRef("/address")).
				Build(),
			EventsConfiguration{},
			`{"kind": "org", "key": "my-key",
				"name": "my-name", "_meta": {"redactedAttributes": ["/address"]}}`,
		},
		{
			"attribute name is escaped if necessary in redactedAttributes",
			ldcontext.NewBuilder("my-key").Kind("org").
				SetString("/a/b~c", "value").
				Build(),
			EventsConfiguration{AllAttributesPrivate: true},
			`{"kind": "org", "key": "my-key",
				"_meta": {"redactedAttributes": ["/~1a~1b~0c"]}}`,
		},
		{
			"meta has both private and secondary",
			ldcontext.NewBuilder("my-key").Kind("org").
				Name("x").
				Secondary("y").
				Private("name").
				Build(),
			EventsConfiguration{AllAttributesPrivate: true},
			`{"kind": "org", "key": "my-key",
				"_meta": {"redactedAttributes": ["name"], "secondary": "y"}}`,
		},
	} {
		t.Run(p.desc, func(t *testing.T) {
			f := newEventContextFormatter(p.options)
			w := jwriter.NewWriter()
			f.WriteContext(&w, &EventContext{Context: p.context})
			require.NoError(t, w.Error())
			actualJSON := sortPrivateAttributesInOutputJSON(w.Bytes())
			m.In(t).Assert(actualJSON, m.JSONStrEqual(p.expectedJSON))
		})
	}
}

func TestWriteInvalidContext(t *testing.T) {
	badContext := ldcontext.New("")
	f := newEventContextFormatter(EventsConfiguration{})
	w := jwriter.NewWriter()
	f.WriteContext(&w, &EventContext{Context: badContext})
	assert.Equal(t, badContext.Err(), w.Error())
}

func sortPrivateAttributesInOutputJSON(data []byte) []byte {
	parsed := ldvalue.Parse(data)
	if parsed.Type() != ldvalue.ObjectType {
		return data
	}
	var ret ldvalue.Value
	if parsed.GetByKey("kind").StringValue() != "multi" {
		ret = sortPrivateAttributesInSingleKind(parsed)
	} else {
		out := ldvalue.ObjectBuildWithCapacity(parsed.Count())
		for k, v := range parsed.AsValueMap().AsMap() {
			if k == "kind" {
				out.Set(k, v)
			} else {
				out.Set(k, sortPrivateAttributesInSingleKind(v))
			}
		}
		ret = out.Build()
	}
	return []byte(ret.JSONString())
}

func sortPrivateAttributesInSingleKind(parsed ldvalue.Value) ldvalue.Value {
	out := ldvalue.ObjectBuildWithCapacity(parsed.Count())
	for k, v := range parsed.AsValueMap().AsMap() {
		if k != "_meta" {
			out.Set(k, v)
			continue
		}
		outMeta := ldvalue.ObjectBuildWithCapacity(v.Count())
		for k1, v1 := range v.AsValueMap().AsMap() {
			if k1 != "redactedAttributes" {
				outMeta.Set(k1, v1)
				continue
			}
			values := v1.AsValueArray().AsSlice()
			sort.Slice(values, func(i, j int) bool {
				return values[i].StringValue() < values[j].StringValue()
			})
			outMeta.Set(k1, ldvalue.ArrayOf(values...))
		}
		out.Set(k, outMeta.Build())
	}
	return out.Build()
}
