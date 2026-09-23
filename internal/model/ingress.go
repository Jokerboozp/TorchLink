package model /* 声明 model 包。 */

import "errors" /* 引入当前代码需要的依赖。 */

var ErrNotFound = errors.New("not found")                     /* 声明 ErrNotFound。 */
var ErrInvalidIngress = errors.New("invalid ingress message") /* 声明 ErrInvalidIngress。 */
