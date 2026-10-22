package workbench

import "testing"

// 只取 eSpeak 动态库、eSpeak 数据和 MeCab 动态库，Python 扩展与元数据不进入发音库目录。
func TestKokoroFile(t *testing.T) {
	libraries := [2]string{"libespeak-ng.dylib", "libmecab.2.dylib"}
	for _, test := range []struct{ project, name, want string }{
		{"espeakng-loader", "espeakng_loader/libespeak-ng.dylib", "libespeak-ng.dylib"},
		{"espeakng-loader", "espeakng_loader/libespeak-ng.1.dylib", ""},
		{"espeakng-loader", "espeakng_loader/espeak-ng-data/voices/!v/zac", "espeak-ng-data/voices/!v/zac"},
		{"espeakng-loader", "espeakng_loader/espeak-ng-data/", ""},
		{"espeakng-loader", "espeakng_loader/__init__.py", ""},
		{"fugashi", "fugashi/.dylibs/libmecab.2.dylib", "libmecab.2.dylib"},
		{"fugashi", "fugashi.libs/libmecab-eada4a80.so.2.0.0", "libmecab.2.dylib"},
		{"fugashi", "fugashi.libs/libmecab-d8ddc0791437ce8c4a187ae7c69a2f68.dll", "libmecab.2.dylib"},
		{"fugashi", "fugashi/fugashi.cpython-311-darwin.so", ""},
		{"fugashi", "fugashi-1.5.2.dist-info/licenses/LICENSE.mecab", ""},
	} {
		if got := kokoroFile(test.project, test.name, libraries); got != test.want {
			t.Errorf("%s %s：期望 %q，得到 %q", test.project, test.name, test.want, got)
		}
	}
}
