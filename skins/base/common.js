'use strict';

// https://jsbin.com/qizepev/edit?html,css,js,output
function enableAutoExpand() {

	let textareas = document.querySelectorAll('.autoexpand');
	for (let i=0; i<textareas.length; i++) {
		textareas[i].classList.add('autoexpand-enabled');

		let hiddenDiv = document.createElement('div');
		hiddenDiv.classList.add('autoexpand-hidden');
		textareas[i].parentNode.insertBefore(hiddenDiv, textareas[i]);

		let cs = getComputedStyle(textareas[i]);
		let textareaMargin = parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight) + parseFloat(cs.borderRightWidth);

		textareas[i].addEventListener('input', function () {
			let content = this.value;
			hiddenDiv.style.width = (this.getBoundingClientRect().width - textareaMargin).toFixed(3) + 'px';
			hiddenDiv.textContent = content + '\n\n';
			this.style.height = hiddenDiv.getBoundingClientRect().height + 'px';
		});
		textareas[i].dispatchEvent(new Event('input'));
	}
}

function onLoad() {
	enableAutoExpand();
}

if (document.readyState == 'interactive' || document.readyState == 'complete') {
	onLoad();
} else {
	document.addEventListener('DOMContentLoaded', onLoad());
}
