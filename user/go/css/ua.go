package css

// UA defaults are derived from the pre-M93 StyleFor/BlockElement tag tables,
// using only the frozen subset. Legacy underline/accent-bar decorations have no
// CSS property in this subset and are not invented here. The noscript fallback
// is visible, as required by Amendment D.
const uaSource = `
html,body,document { display:block; color:#e6edf3; }
body { background-color:#182026; }
div,section,article,header,footer,main,nav,aside,figure { display:block; margin-bottom:4px; }
h1 { display:block; font-size:26px; font-weight:bold; margin:10px 0 8px; }
h2 { display:block; font-size:26px; font-weight:bold; margin:8px 0 6px; }
h3 { display:block; font-weight:bold; margin:8px 0 5px; }
h4,h5,h6 { display:block; font-weight:bold; color:#8b98a5; margin:6px 0 4px; }
p { display:block; margin-bottom:8px; }
figcaption { display:block; color:#8b98a5; margin-bottom:6px; }
ul,ol { display:block; padding-left:8px; margin:2px 0 8px; }
li { display:list-item; padding-left:12px; margin-bottom:3px; }
dl { display:block; margin-bottom:8px; }
dt { display:block; font-weight:bold; margin-bottom:2px; }
dd { display:block; color:#8b98a5; padding-left:12px; margin-bottom:4px; }
pre { display:block; font-family:monospace; white-space:pre; background-color:#222d35; margin:4px 0 8px; padding-left:6px; }
code,kbd,samp,tt { font-family:monospace; background-color:#222d35; }
blockquote { display:block; color:#8b98a5; padding-left:14px; margin:4px 0 8px; }
hr { display:block; color:#2e3a44; margin:8px 0; }
a { color:#3b82f6; }
strong,b { font-weight:bold; }
em,i { font-style:italic; color:#3b82f6; }
small { color:#8b98a5; }
table { display:table; margin:4px 0 8px; }
thead { display:table-header-group; }
tbody { display:table-row-group; }
tfoot { display:table-footer-group; }
tr { display:table-row; }
td,th { display:table-cell; }
th { font-weight:bold; }
caption { display:table-caption; }
img { display:block; color:#8b98a5; margin:4px 0 8px; }
form { display:block; margin:4px 0 8px; }
fieldset { display:block; margin:6px 0 8px; padding-left:4px; }
legend { display:block; font-weight:bold; margin-bottom:4px; }
input,textarea,select,button { background-color:#222d35; margin-bottom:4px; }
textarea,select,button { display:block; }
address { display:block; color:#8b98a5; margin:4px 0 8px; }
center { display:block; text-align:center; }
script,style,head,title,meta,link,template { display:none; }
`

var uaSheet = func() *Stylesheet {
	s, _ := Parse([]byte(uaSource))
	return s
}()
